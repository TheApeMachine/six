package mesh

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
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
	values  []*primitive.Value
	rb      *ringbuffer.RingBuffer
	id      uint64
	modulus uint32
	fields  []*Field
	fingers [][primitive.AffinityWords]uint64
	seed    [primitive.AffinityWords]uint64
}

/*
NewField creates a new Field with the given modulus.
queue must be non-nil: gossip.Conn shares pool.Queue
(scheduler + stream sink).
*/
func NewField(
	ctx context.Context,
	modulus uint32,
) *Field {
	ctx, cancel := context.WithCancel(ctx)

	field := &Field{
		ctx:     ctx,
		cancel:  cancel,
		values:  make([]*primitive.Value, 0),
		id:      fieldIDSeq.Add(1),
		modulus: modulus,
		fields:  make([]*Field, 0),
	}

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
		readers := make([]io.Reader, 0, len(field.values))

		for _, value := range field.values {
			readers = append(readers, value)
		}

		return io.MultiReader(readers...).Read(p)
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

	select {
	case <-field.ctx.Done():
		return 0, io.EOF
	default:
		value := primitive.AllocValue()
		defer value.Close()

		if err := value.LoadFullFrame(p); err != nil {
			return 0, errnie.Error(err)
		}

		field.values = append(field.values, value)
		return len(p), nil
	}
}
