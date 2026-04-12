package pool

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

/*
Slot is a single slot for a worker in Pool.
*/
type Slot struct {
	threadPtr unsafe.Pointer
	task      func()
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
	top atomic.Pointer[Node]
	_p3 [cacheLinePadSize - unsafe.Sizeof(atomic.Pointer[Node]{})]byte
}

// NewPool returns a new thread pool
func NewPool(size uint64) *Pool {
	return &Pool{maxSize: size}
}

// Submit submits a new task to the pool
// it first tries to use already parked goroutines from the stack if any
// if there are no available worker goroutines, it tries to add a
// new goroutine to the pool if the pool capacity is not exceeded
// in case the pool capacity hit its maximum limit, this function yields the processor to other
// goroutines and loops again for finding available workers
func (self *Pool) Submit(task func()) {
	var slot *Slot

	for {
		if slot = self.pop(); slot != nil {
			slot.task = task
			safe_ready(slot.threadPtr)
			return
		} else if atomic.AddUint64(&self.currSize, 1) <= self.maxSize {
			slot = &Slot{task: task}
			go self.loopQ(slot)
			return
		} else {
			atomic.AddUint64(&self.currSize, ^uint64(0)) // Subtract 1
			// Direct gosched via assembly avoids package init ordering issues; same cooperative yield as runtime.Gosched.
			mcall(gosched_m)
		}
	}
}

// loopQ is the looping function for every worker goroutine
func (self *Pool) loopQ(slot *Slot) {
	// store self goroutine pointer
	slot.threadPtr = GetG()
	for {
		// exec task
		slot.task()
		// notify availability by pushing self reference into stack
		self.push(slot)
		// park and wait for call
		mcall(fast_park)
	}
}

// global memory pool for all items used in Pool
var (
	itemPool  = sync.Pool{New: func() any { return new(Node) }}
	itemAlloc = itemPool.Get
	itemFree  = itemPool.Put
)

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
			top.value = nil
			top.next.Store(nil)
			itemFree(top)
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
		item = itemAlloc().(*Node)
	)

	item.value = value

	for {
		top = self.top.Load()
		item.next.Store(top)

		if self.top.CompareAndSwap(top, item) {
			return
		}
	}
}
