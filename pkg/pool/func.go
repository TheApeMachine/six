package pool

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

type (
	// a single slot for a worker in PoolWithFunc
	SlotFunc[T any] struct {
		threadPtr unsafe.Pointer
		data      T
	}

	// PoolWithFunc is used for spawning workers for a single pre-defined function with myriad inputs
	// useful for throughput bound cases
	// has lower memory usage and allocs per op than the default Pool
	//
	//	( type -> func(T) {} ) where T is a generic parameter
	PoolWithFunc[T any] struct {
		currSize uint64
		_p1      [cacheLinePadSize - unsafe.Sizeof(uint64(0))]byte
		maxSize  uint64
		alloc    func() any
		free     func(any)
		task     func(T)
		_p2      [cacheLinePadSize - unsafe.Sizeof(uint64(0)) - 3*unsafe.Sizeof(func() {})]byte
		top      atomic.Pointer[DataItem[T]]
		_p3      [cacheLinePadSize - unsafe.Sizeof(
			atomic.Pointer[DataItem[T]]{},
		)]byte
	}
)

/*
NewPoolWithFunc returns a new PoolWithFunc
*/
func NewPoolWithFunc[T any](size uint64, task func(T)) *PoolWithFunc[T] {
	dataPool := sync.Pool{
		New: func() any { return new(DataItem[T]) },
	}

	return &PoolWithFunc[T]{
		maxSize: size,
		task:    task,
		alloc:   dataPool.Get,
		free:    dataPool.Put,
	}
}

/*
Invoke invokes the pre-defined method in PoolWithFunc by assigning the data to an already existing worker
or spawning a new worker given queue size is in limits
*/
func (self *PoolWithFunc[T]) Invoke(value T) {
	var slot *SlotFunc[T]

	for {
		if slot = self.pop(); slot != nil {
			slot.data = value
			safe_ready(slot.threadPtr)
			return
		} else if atomic.AddUint64(&self.currSize, 1) <= self.maxSize {
			slot = &SlotFunc[T]{data: value}
			go self.loopQ(slot)
			return
		} else {
			atomic.AddUint64(&self.currSize, uint64SubtractionConstant)
			mcall(gosched_m)
		}
	}
}

/*
loopQ represents the infinite loop for a worker goroutine
*/
func (self *PoolWithFunc[T]) loopQ(sf *SlotFunc[T]) {
	sf.threadPtr = GetG()

	for {
		self.task(sf.data)
		self.push(sf)
		mcall(fast_park)
	}
}

/*
DataItem is a single node in the stack
*/
type DataItem[T any] struct {
	next  atomic.Pointer[DataItem[T]]
	value *SlotFunc[T]
}

/*
pop pops value from the top of the stack
*/
func (self *PoolWithFunc[T]) pop() (value *SlotFunc[T]) {
	var top, next *DataItem[T]

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
			self.free(top)
			return
		}
	}
}

/*
push pushes a value on top of the stack
*/
func (self *PoolWithFunc[T]) push(value *SlotFunc[T]) {
	var (
		top  *DataItem[T]
		item = self.alloc().(*DataItem[T])
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

