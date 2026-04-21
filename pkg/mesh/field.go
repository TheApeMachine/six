package mesh

import (
	"bytes"
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
	"github.com/theapemachine/six/pkg/transport"
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

	// We set an initial buffer to ensure that the conn's pass-through
	// will not block. This is the correct behavior for the global
	// field that does not have communities yet, and for the community
	// fields that do not have any values yet.
	conn, err := gossip.NewConn(
		ctx,
		queue,
		telemetry,
		transport.NewCollector(core.Cfg.Value.Bytes),
	)

	if errnie.Error(err) != nil {
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
		conn:        conn,
		queue:       queue,
		telemetry:   telemetry,
		snap:        &geometry.EigenSnap{},
		dial:        geometry.NewPhaseDial(),
		routeBudget: core.Cfg.System.RouteBudget,
		policy:      policy,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
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
	return field.conn.Read(p)
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
	if field == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, errors.Join(
			io.ErrShortWrite,
			errors.New("field.Write: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	visitor := primitive.AllocValue()

	if err := visitor.LoadFullFrame(p); err != nil {
		errnie.Error(errors.Join(
			io.ErrShortBuffer,
			errors.New("field.Write: visitor.LoadFullFrame(p) failed"),
		))

		return 0, err
	}

	field.findCommunity(visitor)

	field.metrics.Load().Refresh(field)

	return field.conn.Write(visitor.Bytes())
}

func (field *Field) findCommunity(visitor *primitive.Value) {
	// Check if the visitor is already a member of a field.
	if community, err := visitor.Property(primitive.COMMUNITY); err == nil && community != 0 {
		// The visitor is already a member of a field.
		for _, f := range field.fields {
			if f.id == community {
				f.metrics.Load().Refresh(f)
				break
			}
		}
		return
	}

	// Find the community that the visitor belongs to.
	for _, f := range field.fields {
		// Compare the field's affinity with the visitor's affinity,
		// and check if the visitor's affinity is within the Hamming
		// distance of the field's affinity.
		visitorAffinity := visitor.Get(primitive.AffinityRegion)

		if len(visitorAffinity) == 0 {
			continue
		}

		hammingDistance := uint64(0)
		maxWords := max(len(visitorAffinity), len(f.affinity))

		for i := range maxWords {
			fieldWord := uint64(0)

			if i < len(f.affinity) {
				fieldWord = f.affinity[i].Load()
			}

			visitorWord := uint64(0)

			if i < len(visitorAffinity) {
				visitorWord = visitorAffinity[i]
			}

			hammingDistance += uint64(
				bits.OnesCount64(fieldWord ^ visitorWord),
			)
		}

		if hammingDistance <= uint64(field.routeBudget) {
			f.values = append(f.values, visitor)

			// Update the field's affinity with the visitor's affinity.
			f.foldAffinityXOR(visitor)

			// Stamp the visitor with the field's ID.
			visitor.Set(
				core.Cfg.Value.Region.Properties.Start+int(primitive.COMMUNITY),
				f.id,
			)

			// Mark the visitor as READY so it can be executed by the ALU.
			visitor.Set(
				core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS),
				uint64(primitive.READY),
			)

			f.conn.Update(bytes.NewBuffer(visitor.Bytes()))
			f.metrics.Load().Refresh(f)
			break
		}
	}

	// Check if the visitor is now a member of a field.
	if community, err := visitor.Property(primitive.COMMUNITY); err == nil && community != 0 {
		// The visitor was assigned to a field above.
		return
	}

	// If not, we need to create a new field.
	newField := NewField(field.ctx, field.modulus, field.telemetry, field.queue)
	newField.values = append(newField.values, visitor)

	// Update the new field's affinity with the visitor's affinity.
	newField.foldAffinityXOR(visitor)

	// Update the new field's conn.
	newField.conn.Update(bytes.NewBuffer(visitor.Bytes()))

	field.fields = append(field.fields, newField)

	field.conn.Update(newField.conn)

	// Stamp the visitor with the new field's ID.
	visitor.Set(
		core.Cfg.Value.Region.Properties.Start+int(primitive.COMMUNITY), newField.id,
	)

	// Mark the visitor as READY so it can be executed by the ALU.
	visitor.Set(
		core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS),
		uint64(primitive.READY),
	)
}
