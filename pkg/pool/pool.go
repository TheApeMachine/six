package pool

import (
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Slot is a single slot for a worker in Pool.
*/
type Slot struct {
	threadPtr unsafe.Pointer
	task      *primitive.Value
	fn        func()
}

/*
Pool represents the thread-pool for performing
any kind of task ( type -> func() {} )
*/
type Pool struct {
	currSize uint64
	_p1      [cacheLinePadSize - unsafe.Sizeof(uint64(0))]byte
	maxSize  uint64
	_p2      [cacheLinePadSize - unsafe.Sizeof(uint64(0))]byte
	// using a stack keeps cpu caches warm based on FILO property
	top      atomic.Pointer[Node]
	_p3      [cacheLinePadSize - unsafe.Sizeof(atomic.Pointer[Node]{})]byte
	dispatch func(*primitive.Value)
}

/*
NewPool returns a new thread pool. The optional dispatch handler receives
every non-nil Executable returned by a task — this is how the compute
Backend picks up work without the task closure knowing about substrates.
*/
func NewPool(size uint64) *Pool {
	return &Pool{maxSize: size}
}

// Schedule submits a raw function to be executed by a pool worker.
// Mirrors Submit but for arbitrary closures rather than ALU Values.
func (self *Pool) Schedule(fn func()) {
	if fn == nil {
		return
	}

	var slot *Slot

	for {
		if slot = self.pop(); slot != nil {
			slot.fn = fn
			safe_ready(slot.threadPtr)
			return
		} else if atomic.AddUint64(&self.currSize, 1) <= self.maxSize {
			slot = &Slot{fn: fn}
			go self.loopQ(slot)
			return
		} else {
			atomic.AddUint64(&self.currSize, ^uint64(0)) // Subtract 1
			mcall(gosched_m)
		}
	}
}

func (self *Pool) loopQ(slot *Slot) {
	slot.threadPtr = GetG()

	for {
		if fn := slot.fn; fn != nil {
			fn()
			slot.fn = nil
		} else if value := slot.task; value != nil && self.dispatch != nil {
			self.dispatch(value)
		}

		slot.task = nil
		self.push(slot)
		mcall(fast_park)
	}
}

// global memory pool for all items used in Pool
// (removed to avoid ABA problem and use-after-free)

/*
Node is a single node in this stack
internal lock-free stack implementation for parking and waking up goroutines
Credits -> https://github.com/golang-design/lockfree
*/
type Node struct {
	next  atomic.Pointer[Node]
	value *Slot
}

/*
pop a value from the top of the stack
*/
func (self *Pool) pop() (value *Slot) {
	var top, next *Node

	for {
		top = self.top.Load()

		if top == nil {
			return
		}

		next = top.next.Load()

		if self.top.CompareAndSwap(top, next) {
			value = top.value
			return
		}
	}
}

/*
push pushes a value on top of the stack
*/
func (self *Pool) push(value *Slot) {
	var (
		top  *Node
		item = &Node{value: value}
	)

	for {
		top = self.top.Load()
		item.next.Store(top)

		if self.top.CompareAndSwap(top, item) {
			return
		}
	}
}
