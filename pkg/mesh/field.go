package mesh

import (
	"context"
	"fmt"
	"io"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
affinityWords is the fixed uint64 width of a Value's affinity region. It
mirrors value.region.affinity.bits=257 rounded up; hardcoding the length
lets the scan code run against [affinityWords]uint64 arrays, which the
compiler keeps in registers and the cache keeps L1-resident. A config
mismatch is fatal at init because a silent width divergence would produce
wrong Hamming scores and silently mis-route every Value.
*/
const affinityWords = 5

/*
communityIDOffset is the word offset within the Properties region where
the routing parent stamps the community index after Write selects or
spawns a sub-Field. It sits at the start of the 1024-bit extension
(properties was 512 bits, now 1536) so the legacy lower-half layout
stays untouched. The visualizer reads this word straight off the wire
frame so the rendering path has zero extra RTTs.
*/
const communityIDOffset = 8

func init() {
	_, cfgWords := core.Cfg.Value.Region.Affinity.WordExtent()

	if cfgWords != affinityWords {
		panic(fmt.Sprintf(
			"mesh: affinity word mismatch — cfg=%d, const=%d (update mesh.affinityWords together with value.region.affinity.bits)",
			cfgWords, affinityWords,
		))
	}
}

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
	ctx    context.Context
	cancel context.CancelFunc
	err    error

	modulus uint32

	fields  []*Field
	fingers [][affinityWords]uint64

	values []*primitive.Value

	// affinity is the XOR fold of every Value's affinity region that this
	// Field has ever observed (directly via AddValue, or indirectly via
	// Write when the Field is a leaf). The fold commutes and associates so
	// parent.affinity == XOR(child.affinity ∀ children) drops out for free.
	affinity [affinityWords]uint64

	// seed is the routing centroid: the affinity of the first Value that
	// arrived through Write at this Field. Unlike the XOR aggregate it is
	// stable as membership grows, which keeps Hamming-distance routing
	// meaningful past the point where an XOR fold would look random.
	seed       [affinityWords]uint64
	seedPinned bool

	snap *geometry.EigenSnap
	dial geometry.PhaseDial

	mu         sync.Mutex
	readCursor atomic.Uint64

	// childModulus is the GF modulus newly-spawned sub-Fields are
	// constructed at; zero disables routing so Write falls back to the
	// leaf path (AddValue on self).
	childModulus uint32

	// routeBudget is the inclusive Hamming-distance ceiling for joining an
	// existing sub-Field. Any candidate whose seed distance exceeds this
	// number is rejected in favor of spawning a fresh community.
	routeBudget int
}

/*
FieldOption configures optional behavior on a Field. The zero-option
form of NewField keeps the historical leaf-only semantics so existing
tests and call sites are unaffected.
*/
type FieldOption func(*Field)

/*
WithCommunities switches a Field into a routing parent. Incoming
Write calls decode the frame, then compare the Value's affinity
against every existing sub-Field's seed; the closest sub-Field within
budget wins, and a cold-miss spawns a new sub-Field at childModulus
seeded by the incoming Value. Budget is Hamming bits over the full
257-bit affinity region; a budget of 48 (~18.7%) is a sensible
starting point for topical clustering.
*/
func WithCommunities(childModulus uint32, routeBudget int) FieldOption {
	return func(field *Field) {
		field.childModulus = childModulus
		field.routeBudget = routeBudget
	}
}

/*
NewField creates a new Field with the given modulus. Options tune
optional behaviour; without any options the Field is a leaf and
Write stores Values directly on it.
*/
func NewField(
	ctx context.Context, modulus uint32, options ...FieldOption,
) *Field {
	ctx, cancel := context.WithCancel(ctx)

	_, propsWords := core.Cfg.Value.Region.Properties.WordExtent()

	if propsWords <= communityIDOffset {
		panic(fmt.Sprintf(
			"mesh: properties region too narrow for communityIDOffset=%d (need >%d words, have %d)",
			communityIDOffset, communityIDOffset, propsWords,
		))
	}

	field := &Field{
		ctx:     ctx,
		cancel:  cancel,
		modulus: modulus,
		snap:    &geometry.EigenSnap{},
		dial:    geometry.NewPhaseDial(),
	}

	for _, option := range options {
		if option != nil {
			option(field)
		}
	}

	return field
}

/*
Close closes the Field and all of its sub-fields. Values are not
closed here: they may still be held by gossip.Conn bundles or by
peer Fields. Ownership of Values is the caller's responsibility.
*/
func (field *Field) Close() error {
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
AddField attaches a sub-Field. Used to build community hierarchies.
The packed fingerprint mirror is extended in lockstep so the routing
scan stays cache-dense even when callers hand-build hierarchies.
*/
func (field *Field) AddField(ctx context.Context, modulus uint32) *Field {
	newField := NewField(ctx, modulus)
	field.fields = append(field.fields, newField)
	field.fingers = append(field.fingers, [affinityWords]uint64{})

	return newField
}

/*
Fields returns the sub-Fields attached to this Field. The slice
aliases internal storage; callers must treat it read-only.
*/
func (field *Field) Fields() []*Field {
	return field.fields
}

/*
AddValue registers a Value with the Field and folds its affinity
signature into the aggregate. The fold is a per-word XOR so merging
two populations commutes with merging the Values individually.
*/
func (field *Field) AddValue(value *primitive.Value) {
	if value == nil {
		return
	}

	field.values = append(field.values, value)

	start, _ := core.Cfg.Value.Region.Affinity.WordExtent()

	// Unrolled — the word count is a package constant so the compiler
	// keeps these in registers instead of setting up a slice/loop.
	field.affinity[0] ^= (*value)[start]
	field.affinity[1] ^= (*value)[start+1]
	field.affinity[2] ^= (*value)[start+2]
	field.affinity[3] ^= (*value)[start+3]
	field.affinity[4] ^= (*value)[start+4]
}

/*
Values returns the population backing this Field. The slice aliases
the Field's own storage; callers must treat it read-only.
*/
func (field *Field) Values() []*primitive.Value {
	return field.values
}

/*
Affinity returns the aggregate affinity fingerprint for this Field.
It is the XOR fold of every member Value's affinity region and so
behaves like a Bloom-ish signature: structurally similar populations
converge toward matching fingerprints.
*/
func (field *Field) Affinity() []uint64 {
	return field.affinity[:]
}

/*
Read emits the next stored Value's wire frame in round-robin order.
This is the Value-stream view of a Field: downstream Conns can
io.Copy from a Field and consume its population one frame at a
time. Returns io.EOF when the Field is empty so idiomatic copy
loops terminate naturally.
*/
func (field *Field) Read(p []byte) (n int, err error) {
	if field == nil {
		return 0, io.ErrClosedPipe
	}

	if len(field.values) == 0 {
		return 0, io.EOF
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	cursor := field.readCursor.Add(1) - 1
	idx := cursor % uint64(len(field.values))

	value := field.values[idx]

	if value == nil {
		return 0, io.EOF
	}

	return value.Read(p)
}

/*
Write materializes a Value from the inbound wire frame and either
stores it here (leaf Field) or routes it into the matching sub-Field
(community parent). Routing is a fully unrolled POPCNT sweep over
the packed fingerprint table; see hammingToSeed. The frame is
treated as authoritative: ID and affinity come straight from the
bytes (primitive.ValueFromWireFrame does not re-stamp), matching
the round-trip semantics of Value.Read.
*/
func (field *Field) Write(p []byte) (n int, err error) {
	if field == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	field.mu.Lock()
	defer field.mu.Unlock()

	value, err := primitive.ValueFromWireFrame(p)

	if err != nil {
		return 0, err
	}

	// Pull the Value's affinity into scalar registers once. The scan below
	// re-uses these five locals without touching the Value's 128-word
	// backing store on every child compare — that keeps the inner loop
	// register-bound even for hundreds of children.
	affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()
	t0 := (*value)[affStart]
	t1 := (*value)[affStart+1]
	t2 := (*value)[affStart+2]
	t3 := (*value)[affStart+3]
	t4 := (*value)[affStart+4]

	// Parent aggregate folds every inbound frame regardless of routing so
	// the hierarchy invariant parent.affinity == XOR(child.affinity) and
	// parent.affinity == XOR(member.affinity ∀ members transitively) stays
	// true without a separate rebuild step.
	field.affinity[0] ^= t0
	field.affinity[1] ^= t1
	field.affinity[2] ^= t2
	field.affinity[3] ^= t3
	field.affinity[4] ^= t4

	if field.childModulus == 0 {
		// Leaf Field: preserve the existing contract — Write is
		// decode-then-AddValue, just with the fold already done inline so
		// we avoid re-reading the affinity words.
		field.values = append(field.values, value)

		return core.Cfg.Value.Bytes, nil
	}

	// Routing parent. Pin the seed on first Write so the parent also has
	// a stable centroid if it ever becomes a child of a higher-tier
	// parent in a deeper hierarchy.
	if !field.seedPinned {
		field.seed[0], field.seed[1], field.seed[2], field.seed[3], field.seed[4] = t0, t1, t2, t3, t4
		field.seedPinned = true
	}

	winnerIdx := field.findCommunity(t0, t1, t2, t3, t4)

	var winner *Field

	if winnerIdx < 0 {
		winner = field.AddField(field.ctx, field.childModulus)
		winnerIdx = len(field.fields) - 1

		// Seed the newly-spawned community and mirror that seed into the
		// packed fingerprint table. Seeds never move after this point —
		// stability is what keeps the Hamming distance meaningful as the
		// community grows.
		winner.seed = [affinityWords]uint64{t0, t1, t2, t3, t4}
		winner.seedPinned = true

		field.fingers[winnerIdx] = winner.seed
	} else {
		winner = field.fields[winnerIdx]
	}

	// Fold into the winning child. Inlined rather than delegated to
	// AddValue so we re-use the five registers we already loaded and
	// avoid re-reading the Value's affinity region.
	winner.values = append(winner.values, value)
	winner.affinity[0] ^= t0
	winner.affinity[1] ^= t1
	winner.affinity[2] ^= t2
	winner.affinity[3] ^= t3
	winner.affinity[4] ^= t4

	// Emit an ephemeral Value that carries the community assignment
	// in-band. The ephemeral's properties hold the community index,
	// and its program is a CopyMaskMerge targeting that single word —
	// when the firmware chain processes it, the copy runs in-value
	// through the ALU rather than via a raw Set from mesh code.
	field.emitCommunityTag(winner, winnerIdx)

	return core.Cfg.Value.Bytes, nil
}

/*
emitCommunityTag creates a one-shot ephemeral Value that carries the
community assignment in-band. The ephemeral's properties[communityIDOffset]
holds the community index, and its program region is wired to a
CopyMaskMerge (opcode 0x50) with srcA = srcB = dst = that single word —
an identity copy that routes through the ALU so the Value computes on
itself rather than being mutated from outside. TTL is 1 so the ephemeral
expires after one firmware pass and never leaks past the community
boundary.

The ephemeral is appended to the winner child's value population so a
gossip.Conn reading from the community field naturally picks it up and
stages it into peer Values' Asset regions alongside regular content.
*/
func (field *Field) emitCommunityTag(winner *Field, communityIdx int) {
	tag := primitive.AllocValue()
	tag.StampNewID()

	propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()
	dst := propsStart + communityIDOffset

	// Payload: the community index itself.
	tag.Set(dst, uint64(communityIdx))

	// Program: CopyMaskMerge with srcA = srcB = dst = properties[communityIDOffset, 1].
	// srcB is all-ones (the value word IS the mask) so the merge degenerates
	// to a straight copy: dst = (srcA & srcB) | (dst & ^srcB) → dst = srcA.
	packed := kernel.PackRegionRef(dst, 1)

	tag.Set(kernel.ProgramOpcodeWord, kernel.OpcodeCopyMaskMerge)
	tag.Set(kernel.ProgramSrcAWord, packed)
	tag.Set(kernel.ProgramSrcBWord, packed)
	tag.Set(kernel.ProgramDstWord, packed)

	// TTL = 1: one ALU pass, then the lifecycle hook clears scheduling and
	// the TTLExpiredSentinel prevents re-publication.
	tag.Set(kernel.PropertiesTTLWord, 1)

	winner.values = append(winner.values, tag)
}

/*
findCommunity returns the index of the sub-Field whose routing seed
lies closest in Hamming distance to the given affinity target, or -1
when no seed is within routeBudget.

The scan reads from the packed fingerprint table (fingers) so every
compare is a single 40-byte cache-line-dense load rather than a
pointer chase through *Field heap allocations. bits.OnesCount64
lowers to native POPCNT / CNT+ADDV and the five calls pipeline
independently, so each compare is ~12 cycles. Pruning by best-so-far
keeps the average cost close to the first meaningfully close child
rather than scaling with total community count; exact matches
short-circuit immediately.

Left deliberately non-SIMD: five-word compares per child are already
register-bound, and AVX-512 VPOPCNTQ setup costs outweigh the scan
for community counts in the tens. A transposed SoA layout is the
natural next step when N grows past roughly 64.
*/
func (field *Field) findCommunity(
	t0, t1, t2, t3, t4 uint64,
) int {
	// best is tracked as routeBudget+1 so the first in-budget candidate
	// strictly improves over the sentinel without a separate "found any
	// winner yet" flag.
	best := field.routeBudget + 1
	winnerIdx := -1

	fingers := field.fingers

	for idx := range fingers {
		finger := &fingers[idx]

		score := bits.OnesCount64(t0^finger[0]) +
			bits.OnesCount64(t1^finger[1]) +
			bits.OnesCount64(t2^finger[2]) +
			bits.OnesCount64(t3^finger[3]) +
			bits.OnesCount64(t4^finger[4])

		if score < best {
			best = score
			winnerIdx = idx

			if score == 0 {
				// Exact match — nothing can beat this, short-circuit
				// before touching the rest of the table.
				break
			}
		}
	}

	return winnerIdx
}

/*
Cycle preserves the prior Field surface for upstream callers that
still invoke it as a tick. Under the in-value execution model the
Field itself does not drive programs — bundling a gossip.Conn over
its Values does. Cycle therefore returns the current population as
an observation snapshot and performs no scheduling work.
*/
func (field *Field) Cycle(values ...*primitive.Value) ([]*primitive.Value, error) {
	if field == nil {
		return nil, nil
	}

	for _, value := range values {
		field.AddValue(value)
	}

	return field.values, nil
}
