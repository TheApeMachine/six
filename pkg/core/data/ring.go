package data

import (
	"context"
	"runtime"
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
RingCell is one slot in a Dmitry Vyukov bounded MPMC queue (rigtorp's
MPMCQueue layout): sequence plus payload. Producers and consumers coordinate
only through atomics on sequence, head, and tail — no mutex.

Reference: Vyukov "Bounded MPMC queue" and rigtorp/MPMCQueue (1024cores).
*/
type RingCell struct {
	sequence atomic.Uint64
	data     atomic.Pointer[[128]uint64]
}

/*
Ring is a fixed-capacity multi-producer multi-consumer queue used
as PRIORITY spill storage. Push and Pop are lock-free.
*/
type Ring struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	mask       uint64
	buffer     []RingCell
	enqueuePos atomic.Uint64
	dequeuePos atomic.Uint64
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
			cell.data.Store((*[128]uint64)(ptr))
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
Close closes the ring.
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
