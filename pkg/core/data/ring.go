package data

import (
	"context"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/validate"
)

/*
RingCapacity is the spill slot count (power of two).
Overflowed PRIORITY frames block in pushPriority
until the unified queue drains space; the capacity
bounds worst-case fan-in without unbounded allocations.
*/
const RingCapacity = 65536

/*
ringFrameWords is the fixed payload width of one Vyukov cell, in machine words.
*/
const ringFrameWords = 128

/*
ringPayloadBytes is the byte length of one queued frame (128 × 8).
*/
const ringPayloadBytes = ringFrameWords * 8

/*
RingCell is one slot in a Dmitry Vyukov bounded MPMC queue (rigtorp's
MPMCQueue layout): sequence plus payload. Producers and consumers coordinate
only through atomics on sequence, head, and tail — no mutex.

Reference: Vyukov "Bounded MPMC queue" and rigtorp/MPMCQueue (1024cores).
*/
type RingCell struct {
	sequence atomic.Uint64
	data     atomic.Pointer[[ringFrameWords]uint64]
}

/*
Ring is a fixed-capacity multi-producer multi-consumer queue used
as PRIORITY spill storage. Push and Pop are lock-free.

Read and Write adapt the queue as a byte stream: each Push carries up
to ringPayloadBytes bytes (tail zero-padded). readMu and writeMu serialize
the stream endpoints so partial io.Read/io.Write calls remain coherent;
the queue itself stays lock-free underneath.
*/
type Ring struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	mask       uint64
	buffer     []RingCell
	enqueuePos atomic.Uint64
	dequeuePos atomic.Uint64

	readMu      sync.Mutex
	writeMu     sync.Mutex
	readPending unsafe.Pointer
	readOff     int
}

/*
NewRing allocates a ring. Capacity must be >= 2 and a power of two.
*/
func NewRing(ctx context.Context, capacity int) (*Ring, error) {
	ctx, cancel := context.WithCancel(ctx)

	ring := &Ring{
		ctx:    ctx,
		cancel: cancel,
		mask:   uint64(capacity - 1),
		buffer: make([]RingCell, capacity),
	}

	for index := range ring.buffer {
		ring.buffer[index].sequence.Store(uint64(index))
	}

	return ring, validate.Require(map[string]any{
		"ctx":    ring.ctx,
		"cancel": ring.cancel,
		"mask":   ring.mask,
		"buffer": ring.buffer,
	})
}

/*
ringFrameBytesView maps a frame to its raw byte slice without allocation.
*/
func ringFrameBytes(frame *[ringFrameWords]uint64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(frame)), ringPayloadBytes)
}

/*
pushpopRole selects which side of the Vyukov queue is executing after the
slot CAS: producer publishes a frame; consumer takes one out.
*/
type pushpopRole uint8

const (
	pushpopProducer pushpopRole = 1
	pushpopConsumer pushpopRole = 0
)

/*
pushpop is the shared wait / CAS loop for Push and Pop. The only divergence
after a successful claim is intentional: producers Store into the cell and
advance sequence by one; consumers Swap the payload out and advance by mask+1.
That split is what the earlier unified helper got wrong (always Swap).
*/
func (ring *Ring) pushpop(
	queuePos *atomic.Uint64,
	positionAdd uint64,
	role pushpopRole,
	ptr unsafe.Pointer,
) unsafe.Pointer {
	for {
		position := queuePos.Load()
		cell := &ring.buffer[position&ring.mask]
		seq := cell.sequence.Load()
		diff := int64(seq) - int64(position+positionAdd)

		if diff < 0 {
			return nil
		}

		if diff != 0 {
			runtime.Gosched()
			continue
		}

		if !queuePos.CompareAndSwap(position, position+1) {
			continue
		}

		if role == pushpopProducer {
			cell.data.Store((*[ringFrameWords]uint64)(ptr))
			cell.sequence.Store(position + 1)

			return ptr
		}

		frame := cell.data.Swap(nil)
		cell.sequence.Store(position + ring.mask + 1)

		return unsafe.Pointer(frame)
	}
}

/*
Push enqueues one pointer. Returns false when the ring is full (transient
under contention). Callers that must not drop spin with runtime.Gosched.
*/
func (ring *Ring) Push(ptr unsafe.Pointer) bool {
	if ring == nil {
		return false
	}

	return ring.pushpop(&ring.enqueuePos, 0, pushpopProducer, ptr) != nil
}

/*
Pop dequeues the oldest pointer. Returns nil when empty.
*/
func (ring *Ring) Pop() unsafe.Pointer {
	if ring == nil {
		return nil
	}

	return ring.pushpop(&ring.dequeuePos, 1, pushpopConsumer, nil)
}

/*
Len returns the approximate number of elements between dequeue and enqueue
positions. Used for quiescence checks; under MPMC contention the count is
a lower bound, not a mutex-serialized exact length.
*/
func (ring *Ring) Len() int {
	if ring == nil {
		return 0
	}

	enq := ring.enqueuePos.Load()
	deq := ring.dequeuePos.Load()

	if enq < deq {
		return 0
	}

	return int(enq - deq)
}

/*
Write implements io.Writer. Bytes are chunked into fixed ringPayloadBytes
frames (the last chunk is zero-padded). When the ring is full the call
spins until space appears or the ring is closed.
*/
func (ring *Ring) Write(p []byte) (n int, err error) {
	if ring == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) == 0 {
		return 0, nil
	}

	ring.writeMu.Lock()
	defer ring.writeMu.Unlock()

	if ring.ctx.Err() != nil {
		return 0, io.ErrClosedPipe
	}

	written := 0

	for len(p) > 0 {
		frame := new([ringFrameWords]uint64)
		buf := ringFrameBytes(frame)
		copied := copy(buf, p)

		for !ring.Push(unsafe.Pointer(frame)) {
			if ring.ctx.Err() != nil {
				return written, io.ErrClosedPipe
			}

			runtime.Gosched()
		}

		written += copied
		p = p[copied:]
	}

	return written, nil
}

/*
Read implements io.Reader. It concatenates queued frames into the caller's
slice; a frame shorter than ringPayloadBytes still occupies a full slot on
the wire (padded by the writer). When the queue is empty and the ring is
closed, Read returns io.EOF.
*/
func (ring *Ring) Read(p []byte) (n int, err error) {
	if ring == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) == 0 {
		return 0, nil
	}

	ring.readMu.Lock()
	defer ring.readMu.Unlock()

	total := 0

	for len(p) > 0 {
		if ring.readPending == nil {
			var frame unsafe.Pointer

			for {
				frame = ring.Pop()

				if frame != nil {
					ring.readPending = frame
					ring.readOff = 0

					break
				}

				if ring.ctx.Err() != nil {
					if total > 0 {
						return total, nil
					}

					return 0, io.EOF
				}

				runtime.Gosched()
			}
		}

		active := (*[ringFrameWords]uint64)(ring.readPending)
		buf := ringFrameBytes(active)
		copied := copy(p, buf[ring.readOff:])
		ring.readOff += copied
		total += copied
		p = p[copied:]

		if ring.readOff == len(buf) {
			ring.readPending = nil
			ring.readOff = 0
		}
	}

	return total, nil
}

/*
Close implements io.Closer: it cancels the ring context so blocked Read and
Write calls unwind.
*/
func (ring *Ring) Close() error {
	if ring == nil {
		return nil
	}

	ring.cancel()

	return ring.err
}

/*
Error returns the error of the ring.
*/
func (ring *Ring) Error() error {
	return ring.err
}

var _ io.ReadWriteCloser = (*Ring)(nil)
