package geometry

import (
	"context"
	"io"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
valueFrameSize is the byte length of one primitive.Value wire frame.
[128]uint64 × 8 bytes = 1024 bytes exactly.
*/
const valueFrameSize = 128 * 8

/*
ensureIO initialises the intake and output rings the first time any I/O
method is called. Uses sync.Once so concurrent first-callers are safe.
Both rings use data.RingCapacity (65 536 slots) — the same capacity as the
main pool queue so back-pressure is uniform across the I/O layer.
*/
func (field *Field) ensureIO() {
	field.initIO.Do(func() {
		ctx := context.Background()
		field.intake, _ = data.NewRing(ctx, data.RingCapacity)
		field.output, _ = data.NewRing(ctx, data.RingCapacity)
	})
}

/*
Write implements io.Writer. p must be exactly valueFrameSize bytes
representing one serialised primitive.Value wire frame. The bytes are copied
into a heap-allocated Value and pushed into the lock-free intake ring.
Callers that must not drop frames spin with runtime.Gosched until space is
available — identical to the pool.Queue push contract.
*/
func (field *Field) Write(p []byte) (int, error) {
	if len(p) < valueFrameSize {
		return 0, io.ErrShortBuffer
	}

	field.ensureIO()

	value := new(primitive.Value)

	copy(
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), valueFrameSize),
		p[:valueFrameSize],
	)

	for !field.intake.Push(unsafe.Pointer(value)) {
		runtime.Gosched()
	}

	return valueFrameSize, nil
}

/*
Read implements io.Reader. It pops the next emission from the output ring
and serialises it into p. Returns io.EOF when no emission is pending —
callers must be prepared for a non-blocking empty read rather than blocking.
*/
func (field *Field) Read(p []byte) (int, error) {
	if len(p) < valueFrameSize {
		return 0, io.ErrShortBuffer
	}

	field.ensureIO()

	ptr := field.output.Pop()
	if ptr == nil {
		return 0, io.EOF
	}

	value := (*primitive.Value)(ptr)

	copy(
		p[:valueFrameSize],
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), valueFrameSize),
	)

	return valueFrameSize, nil
}

/*
Close implements io.Closer. Closes both the intake and output rings.
*/
func (field *Field) Close() error {
	if field.intake != nil {
		field.intake.Close()
	}

	if field.output != nil {
		field.output.Close()
	}

	return nil
}

/*
DrainIntake pops all pending Values from the intake ring and appends them to
field.Values so the next Cycle pass picks them up. This is called at the top
of cycleLeaf, making the I/O intake transparent to all existing field logic.
*/
func (field *Field) DrainIntake() {
	if field.intake == nil {
		return
	}

	for {
		ptr := field.intake.Pop()
		if ptr == nil {
			return
		}

		value := (*primitive.Value)(ptr)
		field.Values = append(field.Values, value)
	}
}

/*
EmitValue pushes an emitted Value into the output ring so Read callers can
consume it, and also writes it to every peer in field.peers so
io.MultiWriter-style fan-out is transparent to the emitting code.
Existing callers that pass the emitted Value directly to pool.Queue are
unaffected — EmitValue is an additive path.
*/
func (field *Field) EmitValue(value *primitive.Value) {
	field.ensureIO()

	field.output.Push(unsafe.Pointer(value))

	if len(field.peers) == 0 {
		return
	}

	var buf [valueFrameSize]byte

	copy(
		buf[:],
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), valueFrameSize),
	)

	for _, peer := range field.peers {
		if peer == nil {
			continue
		}

		peer.Write(buf[:])
	}
}

/*
Connect adds an io.ReadWriteCloser peer to this field's fan-out list.
Future EmitValue calls will write to all connected peers. Peers are tried
in the order they were connected; callers that want priority ordering should
wrap the slice in a gossip.PriorityRoute before connecting.
*/
func (field *Field) Connect(peer io.ReadWriteCloser) {
	field.peers = append(field.peers, peer)
}

/*
Disconnect removes the first matching peer from the fan-out list. Identity
comparison by interface value.
*/
func (field *Field) Disconnect(peer io.ReadWriteCloser) {
	for idx, existing := range field.peers {
		if existing == peer {
			field.peers = append(field.peers[:idx], field.peers[idx+1:]...)
			return
		}
	}
}
