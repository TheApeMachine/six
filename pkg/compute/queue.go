package compute

import (
	"context"
	"io"
	"runtime"
	"sync/atomic"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type WorkType int

const (
	WorkTypeValue WorkType = iota
	WorkTypeFunc
)

type WorkItem struct {
	Type     WorkType
	Value    *primitive.Value
	Function func()
}

type workNode struct {
	item WorkItem
	next atomic.Pointer[workNode]
}

type lockFreeQueue struct {
	head   atomic.Pointer[workNode]
	tail   atomic.Pointer[workNode]
	length atomic.Int64
}

func newLockFreeQueue() *lockFreeQueue {
	dummy := &workNode{}
	q := &lockFreeQueue{}
	q.head.Store(dummy)
	q.tail.Store(dummy)
	return q
}

func (q *lockFreeQueue) enqueue(item WorkItem) {
	n := &workNode{item: item}
	for {
		tail := q.tail.Load()
		next := tail.next.Load()
		if tail == q.tail.Load() {
			if next == nil {
				if tail.next.CompareAndSwap(next, n) {
					q.tail.CompareAndSwap(tail, n)
					q.length.Add(1)
					return
				}
			} else {
				q.tail.CompareAndSwap(tail, next)
			}
		}
	}
}

func (q *lockFreeQueue) dequeue() (WorkItem, bool) {
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		next := head.next.Load()
		if head == q.head.Load() {
			if head == tail {
				if next == nil {
					return WorkItem{}, false
				}
				q.tail.CompareAndSwap(tail, next)
			} else {
				item := next.item
				if q.head.CompareAndSwap(head, next) {
					next.item = WorkItem{} // clear reference to avoid memory leaks
					q.length.Add(-1)
					return item, true
				}
			}
		}
	}
}

/*
Scheduler is the universal work scheduler interface.
*/
type Scheduler interface {
	io.ReadWriteCloser
	Submit(value *primitive.Value)
	Schedule(fn func())
	Error() error
}

type Queue struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	rb       *ringbuffer.RingBuffer
	normal   *lockFreeQueue
	priority *lockFreeQueue
}

func NewQueue(ctx context.Context) *Queue {
	ctx, cancel := context.WithCancel(ctx)

	rb := ringbuffer.New(core.Cfg.Value.Bytes * 64)

	q := &Queue{
		ctx:      ctx,
		cancel:   cancel,
		rb:       rb,
		normal:   newLockFreeQueue(),
		priority: newLockFreeQueue(),
	}

	return q
}

/*
Read implements io.Reader. Since the Queue is now the input stage of the
pipeline, it doesn't output directly via Read anymore. The Backend handles
the output stream.
*/
func (q *Queue) Read(p []byte) (n int, err error) {
	errnie.Trace("compute.Queue.Read")

	select {
	case <-q.ctx.Done():
		return 0, q.ctx.Err()
	default:
		return q.rb.Read(p)
	}
}

/*
Write implements io.Writer. It accepts incoming data from the
IO pipeline, converts it to a task, and queues it.
*/
func (q *Queue) Write(p []byte) (n int, err error) {
	errnie.Trace("compute.Queue.Write")

	select {
	case <-q.ctx.Done():
		return 0, q.ctx.Err()
	default:
		value := primitive.AllocValue()

		if err := value.LoadFullFrame(p); err != nil {
			primitive.FreeValue(value)
			return 0, err
		}

		status, err := value.Property(primitive.STATUS)

		if err == nil && status == uint64(primitive.READY) {
			q.Submit(value)
			return len(p), nil
		} else {
			primitive.FreeValue(value)
			return len(p), nil
		}
	}
}

/*
Return writes a computed result back to the queue's output stream.
This ensures that when we read from the queue, we are reading results only.
*/
func (q *Queue) Return(value *primitive.Value) error {
	errnie.Trace("compute.Queue.Return")

	if q == nil || q.rb == nil || value == nil {
		return nil
	}

	value.Set(
		core.Cfg.Value.Region.Properties.Start+int(primitive.STATUS),
		uint64(primitive.DONE),
	)

	_, err := q.rb.Write(value.Bytes())
	return err
}

/*
Close closes the queue.
*/
func (q *Queue) Close() error {
	q.cancel()
	q.rb.CloseWriter()
	return q.err
}

/*
Error implements the error interface.
*/
func (q *Queue) Error() error {
	return q.err
}

/*
Submit a value to the queue. This will use the normal lane
by default, and the priority lane is used for values which
have a next line set in the program region.
*/
func (q *Queue) Submit(value *primitive.Value) {
	errnie.Trace("compute.Queue.Submit")

	item := WorkItem{Type: WorkTypeValue, Value: value}

	if value.SchedulingNext() != 0 {
		q.priority.enqueue(item)
	} else {
		q.normal.enqueue(item)
	}
}

/*
Schedule a function to be executed by the queue.
*/
func (q *Queue) Schedule(fn func()) {
	errnie.Trace("compute.Queue.Schedule")

	item := WorkItem{Type: WorkTypeFunc, Function: fn}
	q.normal.enqueue(item)
}

/*
Pop blocks until work is available, strictly preferring the priority lane.
*/
func (q *Queue) Pop() (WorkItem, error) {
	errnie.Trace("compute.Queue.Pop")

	for {
		if item, ok := q.priority.dequeue(); ok {
			return item, nil
		}

		if item, ok := q.normal.dequeue(); ok {
			return item, nil
		}

		select {
		case <-q.ctx.Done():
			return WorkItem{}, q.ctx.Err()
		default:
			runtime.Gosched()
		}
	}
}
