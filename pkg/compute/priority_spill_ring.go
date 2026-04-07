package compute

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

/*
spillRingCell is one slot in a Dmitry Vyukov bounded MPMC queue (rigtorp's
MPMCQueue layout): sequence plus payload. Producers and consumers coordinate
only through atomics on sequence, head, and tail — no mutex.

Reference: Vyukov "Bounded MPMC queue" and rigtorp/MPMCQueue (1024cores).
*/
type spillRingCell struct {
	sequence atomic.Uint64
	data     atomic.Pointer[[128]uint64]
}

/*
prioritySpillRing is a fixed-capacity multi-producer multi-consumer queue used
as PRIORITY spill storage. tryPush is lock-free; tryPop is lock-free.

Only runUnifiedQueue (and helpers on that goroutine) call tryPop in this
codebase, while several paths may call tryPush concurrently — MPMC correctness
matters for enqueuePriorityUnsafe vs. drain.
*/
type prioritySpillRing struct {
	mask       uint64
	buffer     []spillRingCell
	enqueuePos atomic.Uint64
	dequeuePos atomic.Uint64
}

/*
newPrioritySpillRing allocates a ring. Capacity must be >= 2 and a power of two.
*/
func newPrioritySpillRing(capacity int) *prioritySpillRing {

	if capacity < 2 || capacity&(capacity-1) != 0 {
		panic("prioritySpillRing: capacity must be a power of two >= 2")
	}

	ring := &prioritySpillRing{
		mask:   uint64(capacity - 1),
		buffer: make([]spillRingCell, capacity),
	}

	for index := range ring.buffer {
		ring.buffer[index].sequence.Store(uint64(index))
	}

	return ring
}

/*
tryPush enqueues one pointer. Returns false when the ring is full (transient
under contention). Callers that must not drop spin with runtime.Gosched.
*/
func (ring *prioritySpillRing) tryPush(ptr unsafe.Pointer) bool {

	if ring == nil {
		return false
	}

	for {
		position := ring.enqueuePos.Load()
		cell := &ring.buffer[position&ring.mask]
		seq := cell.sequence.Load()
		diff := int64(seq) - int64(position)

		if diff < 0 {
			return false
		}

		if diff != 0 {
			runtime.Gosched()

			continue
		}

		if !ring.enqueuePos.CompareAndSwap(position, position+1) {
			continue
		}

		cell.data.Store((*[128]uint64)(ptr))
		cell.sequence.Store(position + 1)

		return true
	}
}

/*
tryPop dequeues the oldest pointer. Returns nil when empty.
*/
func (ring *prioritySpillRing) tryPop() unsafe.Pointer {

	if ring == nil {
		return nil
	}

	for {
		position := ring.dequeuePos.Load()
		cell := &ring.buffer[position&ring.mask]
		seq := cell.sequence.Load()
		diff := int64(seq) - int64(position+1)

		if diff < 0 {
			return nil
		}

		if diff != 0 {
			runtime.Gosched()

			continue
		}

		if !ring.dequeuePos.CompareAndSwap(position, position+1) {
			continue
		}

		frame := cell.data.Swap(nil)
		cell.sequence.Store(position + ring.mask + 1)

		return unsafe.Pointer(frame)
	}
}
