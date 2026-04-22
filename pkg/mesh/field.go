package mesh

import (
	"context"
	"errors"
	"io"
	"math/bits"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/numeric/learned"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
fieldIDSeq hands out stable ids to every *Field. stampOwnership writes
the id into the COMMUNITY property word; 0 means unset so the sequence
starts at 1.
*/
var fieldIDSeq atomic.Uint64

/*
Field lifts the system into the geometric domain. A Field is the
storage for a population of Values plus its aggregate affinity
fingerprint; nested sub-fields carry community hierarchies.

Field is an io.ReadWriteCloser so a bundle in gossip.Conn can pull
from it (io.Copy(conn, field)) or fan out to it (io.Copy(field,
conn)). No goroutines, no locks on the hot path: the affinity fold
is a pure XOR (commutative, associative, idempotent in the bit-flip
sense) and the read cursor is a single atomic.

When constructed WithCommunities, Write routes incoming Values into
sub-Fields by Hamming distance over the affinity region instead of
storing them at this level. The scan kernel is a fully-unrolled
POPCNT sweep over a packed fingerprint table (fingers) held on the
parent so every compare lands in L1 with no pointer chase to the
child Field structs.
*/
type Field struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	id      uint64
	modulus uint32

	/*
		conn is the Field's outbound Conn. Frames that flow through
		Field.Write are also published on conn so anything attached as
		a sink (downstream observer, sibling Field, telemetry probe)
		gets a copy without the Field knowing about its observers.
	*/
	conn *gossip.Conn

	/*
		emit is the upward emit channel. Field.refreshMetrics carriers,
		learner Values, and any other internal emission go here. Set by
		the orchestrator (or parent Field) via SetEmit; nil-safe so leaf
		tests that never set it just drop emissions on the floor.
	*/
	emit      *gossip.Conn
	queue     compute.Scheduler
	telemetry *telemetry.Bridge

	fields  []*Field
	fingers [][primitive.AffinityWords]uint64
	router  compute.AffinityRouter

	values []*primitive.Value

	// affinity words hold the XOR fold of every Value's affinity region
	// observed here; each word is updated with CompareAndSwap XOR so the
	// hot path does not take field.mu (slice append still serializes on mu).
	affinity [primitive.AffinityWords]atomic.Uint64

	// seed is the routing centroid: the affinity of the first Value that
	// arrived through Write at this Field. Unlike the XOR aggregate it is
	// stable as membership grows, which keeps Hamming-distance routing
	// meaningful past the point where an XOR fold would look random.
	seed       [primitive.AffinityWords]uint64
	seedPinned bool

	snap        *geometry.EigenSnap
	dial        geometry.PhaseDial
	metrics     atomic.Pointer[FieldMetrics]
	routeBudget int

	resolvedDropCount atomic.Uint64

	// learnerEmissions bounds unsupervised-learner fan-out per field (see spawnUnsupervisedLearnerConn).
	learnerEmissions atomic.Uint64

	// refreshing / rolling coalesce queued metric work: each Write tries
	// to flip the flag with CompareAndSwap and only schedules when it
	// wins the race. The scheduled task clears the flag after publishing
	// so the next Write that arrives gets to enqueue fresh work. Without
	// this, a Write storm fills the shared pool.Queue with O(N²) eigenmode
	// scans and starves backend.Dispatch — Values then never reach
	// RESOLVED and Orchestrator.Cycle spins.
	refreshing atomic.Bool
	// rolling    atomic.Bool
	// readCursor atomic.Uint64

	lastAction core.FirmwareType
	policy     map[core.FirmwareType]*learned.Weight
	rng        *rand.Rand
	program    *Program
}

/*
NewField creates a new Field with the given modulus.
queue must be non-nil: gossip.Conn shares pool.Queue
(scheduler + stream sink).
*/
func NewField(
	ctx context.Context,
	modulus uint32,
	telemetry *telemetry.Bridge,
	queue compute.Scheduler,
) *Field {
	ctx, cancel := context.WithCancel(ctx)

	if queue == nil {
		errnie.Error(errors.New("mesh.NewField: queue is nil"))
		cancel()
		return nil
	}

	policy := make(map[core.FirmwareType]*learned.Weight, len(core.Cfg.Programs))

	for fw := range core.Cfg.Programs {
		policy[fw] = learned.NewWeight(0.35)
	}

	field := &Field{
		ctx:         ctx,
		cancel:      cancel,
		id:          fieldIDSeq.Add(1),
		modulus:     modulus,
		queue:       queue,
		telemetry:   telemetry,
		snap:        &geometry.EigenSnap{},
		dial:        geometry.NewPhaseDial(),
		routeBudget: core.Cfg.System.RouteBudget,
		policy:      policy,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		program:     NewProgram(),
	}

	field.conn, field.err = gossip.NewConn(
		ctx, queue, telemetry, io.Discard,
	)
	field.metrics.Store(NewFieldMetrics())

	return field
}

/*
Close closes the Field and all of its sub-fields. Values are not
closed here: they may still be held by gossip.Conn bundles or by
peer Fields. Ownership of Values is the caller's responsibility.
*/
func (field *Field) Close() error {
	if field == nil {
		return nil
	}

	field.cancel()

	return nil
}

/*
Error returns the error of the Field.
*/
func (field *Field) Error() error {
	return field.err
}

/*
foldAffinityXOR merges one Value's affinity region into the aggregate
using per-word compare-and-swap XOR — no mutex.
*/
func (field *Field) foldAffinityXOR(value *primitive.Value) {
	if value == nil {
		return
	}
	visitorAffinity := value.Get(primitive.AffinityRegion)
	if len(visitorAffinity) == 0 {
		return
	}

	for wordIdx := range visitorAffinity {
		if wordIdx >= len(field.affinity) {
			break
		}

		contrib := visitorAffinity[wordIdx]
		at := &field.affinity[wordIdx]

		for {
			old := at.Load()
			if at.CompareAndSwap(old, old^contrib) {
				break
			}
		}
	}
}

/*
Read emits the next stored Value's wire frame in round-robin order so
downstream consumers can iterate the population without grabbing the
member mutex. Returns io.EOF when the Field is empty so idiomatic
io.Copy loops terminate naturally; callers that fold per frame must
treat io.EOF as the per-frame delimiter (matches Value.Read).

readCursor is a single atomic so the round-robin walk stays lock-free
on the hot path.
*/
func (field *Field) Read(p []byte) (n int, err error) {
	errnie.Trace("mesh.Field.Read")

	select {
	case <-field.ctx.Done():
		return 0, io.EOF
	default:
		return field.conn.Read(p)
	}
}

/*
Write materializes a Value from the inbound wire frame, folds it into
this Field's aggregate affinity, registers it as a member, and either
routes it into a child community (parent mode) or schedules a local
encounter pass (leaf mode).

Routing parents apply the three spawn rules in routeOrSpawn (no
children yet, no community within the Hamming budget, or the matched
community is already at the Shannon limit). The visitor is then
recursively written into the chosen child Field, where storage and
the power-of-two encounter actually happens.

Leaf Fields run the encounter pass directly: a small set of resident
Values absorb the visitor's S+C+G+P stage via Value.Write and then
run one ALU pass each. This is the "many fold with many" mechanism —
the encounter work is enqueued on the shared queue so K resident
folds proceed in parallel without the Write caller blocking.

The frame is treated as authoritative: ID and affinity come straight
from the bytes (primitive.ValueFromWireFrame does not re-stamp),
matching the round-trip semantics of Value.Read. 	The frame is also
fanned out to field.conn so attached observers see every inbound
frame without the Field knowing about them.
*/
func (field *Field) Write(p []byte) (n int, err error) {
	errnie.Trace("mesh.Field.Write")

	if field == nil || field.conn == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, errors.Join(
			io.ErrShortWrite,
			errors.New("field.Write: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	select {
	case <-field.ctx.Done():
		return 0, io.EOF
	default:
		visitor := primitive.AllocValue()

		if err := visitor.LoadFullFrame(p); err != nil {
			primitive.FreeValue(visitor)
			return 0, errnie.Error(errors.Join(
				io.ErrShortBuffer,
				errors.New("field.Write: visitor.LoadFullFrame(p) failed"),
			))
		}

		// findCommunity either adopts the visitor (appends the *Value into
		// some f.values slice and stamps COMMUNITY) or short-circuits when
		// the frame already carries a non-zero COMMUNITY. Adoption
		// transfers ownership: the receiving community keeps the pointer
		// alive for its lifetime, so we MUST NOT close the visitor in that
		// case — Value.Close zero-fills the slot and returns it to the
		// arena, which would silently corrupt the resident population
		// when the same slot is later handed back from AllocValue. Only
		// non-adopted (short-circuited) frames are returned to the arena.
		adopted := field.findCommunity(visitor)

		field.metrics.Load().Refresh(field)

		// findCommunity stamps COMMUNITY (and may touch other words) on
		// the visitor — re-serialize back into p so gossip, telemetry,
		// and the next pipeline stage see the same routing outcome as
		// the resident population. Read does not mutate the Value, so
		// it remains safe to call after adoption.
		if _, rerr := visitor.Read(p); rerr != nil && !errors.Is(rerr, io.EOF) {
			if !adopted {
				primitive.FreeValue(visitor)
			}
			return 0, errnie.Error(rerr)
		}

		// Publish on the outbound conn so attached observers (queue +
		// telemetry via conn's MultiWriter) see every inbound frame. The
		// conn sink is io.Discard so this does not recurse back into
		// Field.Write.
		if _, werr := field.conn.Write(p); werr != nil && werr != io.EOF {
			if !adopted {
				primitive.FreeValue(visitor)
			}
			return 0, errnie.Error(werr)
		}

		if !adopted {
			primitive.FreeValue(visitor)
		}

		return len(p), nil
	}
}

func (field *Field) assignVisitorToChild(f *Field, visitor *primitive.Value) {
	f.values = append(f.values, visitor)
	f.foldAffinityXOR(visitor)

	visitor.Set(
		core.Cfg.Value.Region.Properties.Start+int(primitive.COMMUNITY), f.id,
	)

	f.conn.Update(visitor)
}

func (field *Field) appendChild(child *Field) {
	if field == nil || child == nil {
		return
	}

	child.SetAffinityDistanceProvider(field.router)
	field.fields = append(field.fields, child)
	field.fingers = append(field.fingers, child.seed)
}

func (field *Field) SetAffinityDistanceProvider(provider compute.AffinityRouter) {
	if field == nil {
		return
	}

	field.router = provider
	for _, child := range field.fields {
		child.SetAffinityDistanceProvider(provider)
	}
}

func (field *Field) affinityDistances(visitor *primitive.Value) []uint32 {
	if field == nil || visitor == nil || len(field.fingers) == 0 {
		return nil
	}

	visitorAffinity := visitor.Get(primitive.AffinityRegion)
	if len(visitorAffinity) < primitive.AffinityWords {
		return nil
	}

	var query [primitive.AffinityWords]uint64
	copy(query[:], visitorAffinity[:primitive.AffinityWords])

	if field.router == nil {
		return nil
	}

	return field.router.AffinityDistances(&query, field.fingers)
}

// findCommunity routes the visitor into a community and reports whether the
// visitor pointer was adopted by some f.values slice. When it returns false
// the caller still owns the *Value and must free it (the visitor's frame
// already declared a non-zero COMMUNITY, so we treat it as a re-route and
// drop our local copy). When it returns true ownership of the *Value has
// transferred to the receiving community and the caller must NOT close it.
func (field *Field) findCommunity(visitor *primitive.Value) bool {
	// Visitor frame already declares a community: nothing to adopt here.
	if community, err := visitor.Property(primitive.COMMUNITY); err == nil && community != 0 {
		return false
	}

	// Among children below the Shannon limit, prefer a strict in-budget
	// match; otherwise fall back to the closest seed by Hamming distance
	// so sparse affinities still land in an existing community instead of
	// spawning one leaf per Value.
	var best *Field
	bestDist := ^uint64(0)
	distances := field.affinityDistances(visitor)

	for idx, f := range field.fields {
		// Saturation gates: skip any child that's full so the visitor
		// either lands in a sibling within the Hamming budget or spawns
		// a fresh community below.
		//
		// 1. XOR-fold popcount approaching ShannonLimit. This trips for
		//    uncorrelated populations whose folded affinity drifts toward
		//    the random-fingerprint mean (~50% set bits).
		// 2. Member-count cap. Highly-correlated populations (e.g. many
		//    fragments of the same prompt) keep the XOR fold near zero
		//    forever — their contributions cancel — so gate (1) never
		//    fires. The hard count cap is the backstop that keeps any
		//    one community from absorbing the entire workload regardless
		//    of fingerprint statistics. The visualiser surfaced the
		//    failure mode as a 3000+ member field; the cap stops it.
		totalAffinity := uint64(0)

		for wordIdx := range f.affinity {
			totalAffinity += uint64(
				bits.OnesCount64(f.affinity[wordIdx].Load()),
			)
		}

		if float64(totalAffinity)/float64(
			core.Cfg.Value.Region.Affinity.Bits,
		) >= core.Cfg.System.ShannonLimit {
			continue
		}

		if cap := core.Cfg.System.MaxMembersPerField; cap > 0 &&
			len(f.values) >= cap {
			continue
		}

		hammingDistance := bestDist
		if idx < len(distances) {
			hammingDistance = uint64(distances[idx])
		} else {
			hammingDistance = 0
			for wordIdx := range visitor.Get(primitive.AffinityRegion) {
				if wordIdx >= len(f.seed) {
					break
				}

				hammingDistance += uint64(
					bits.OnesCount64(
						f.seed[wordIdx] ^ visitor.Get(
							primitive.AffinityRegion,
						)[wordIdx],
					),
				)
			}
		}

		if hammingDistance <= uint64(field.routeBudget) {
			field.assignVisitorToChild(f, visitor)
			return true
		}

		if hammingDistance < bestDist {
			bestDist = hammingDistance
			best = f
		}
	}

	if best != nil {
		field.assignVisitorToChild(best, visitor)
		return true
	}

	// If not, we need to create a new community field.
	newField := NewField(field.ctx, field.modulus, field.telemetry, field.queue)

	// Set the seed of the new field to the visitor's affinity.
	for wordIdx := range visitor.Get(primitive.AffinityRegion) {
		if wordIdx >= len(newField.seed) {
			break
		}
		newField.seed[wordIdx] = visitor.Get(primitive.AffinityRegion)[wordIdx]
	}

	// Update the new field's affinity with the visitor's affinity.
	newField.values = append(newField.values, visitor)

	// Stamp the visitor with the new field's ID.
	visitor.Set(
		core.Cfg.Value.Region.Properties.Start+int(primitive.COMMUNITY), newField.id,
	)

	// Update the new field's affinity with the visitor's affinity.
	newField.foldAffinityXOR(visitor)

	// Append the new field to the global field, and make sure to
	// update the conn of the global field, so the values in the new
	// field are folded with all the other fields.
	field.appendChild(newField)

	newField.conn.Update(visitor)
	field.conn.Update(newField)

	return true
}
