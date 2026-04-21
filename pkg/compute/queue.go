package compute

import (
	"context"
	"fmt"
	"io"

	"github.com/theapemachine/six/pkg/core/data"
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
	normal   chan WorkItem
	priority chan WorkItem
	stream   *data.Ring
}

func NewQueue(ctx context.Context) *Queue {
	ctx, cancel := context.WithCancel(ctx)

	stream, err := data.NewRing(ctx, 1024)

	if err != nil {
		cancel()
		return nil
	}

	q := &Queue{
		ctx:      ctx,
		cancel:   cancel,
		normal:   make(chan WorkItem, 100000),
		priority: make(chan WorkItem, 100000),
		stream:   stream,
	}

	return q
}

/*
Read implements io.Reader. Since the Queue is now the input stage of the
pipeline, it doesn't output directly via Read anymore. The Backend handles
the output stream.
*/
func (q *Queue) Read(p []byte) (n int, err error) {
	return q.stream.Read(p)
}

/*
StreamLen returns the number of bytes currently in the stream ring.
*/
func (q *Queue) StreamLen() int {
	if q == nil || q.stream == nil {
		return 0
	}
	return q.stream.Len()
}

/*
Len returns the number of items currently in the normal and priority queues.
*/
func (q *Queue) Len() int {
	if q == nil {
		return 0
	}
	return len(q.normal) + len(q.priority)
}

/*
Write implements io.Writer. It accepts incoming data from the
IO pipeline, converts it to a task, and queues it.
*/
func (q *Queue) Write(p []byte) (n int, err error) {
	value := primitive.AllocValue()

	if err := value.LoadFullFrame(p); err != nil {
		primitive.FreeValue(value)
		return 0, err
	}

	status, err := value.Property(primitive.STATUS)
	fmt.Println("queue.Write received status:", status)
	if err == nil && status == uint64(primitive.READY) {
		q.Submit(value)
		return q.stream.Write(p)
	} else {
		primitive.FreeValue(value)
		return len(p), nil
	}
}

/*
Close closes the queue.
*/
func (q *Queue) Close() error {
	q.cancel()
	return nil
}

/*
Error returns the queue error to satisfy pool.Scheduler.
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
	item := WorkItem{Type: WorkTypeValue, Value: value}

	if value.SchedulingNext() != 0 {
		select {
		case <-q.ctx.Done():
		case q.priority <- item:
		}
		return
	}

	select {
	case <-q.ctx.Done():
	case q.normal <- item:
	}
}

/*
Schedule a function to be executed by the queue.
*/
func (q *Queue) Schedule(fn func()) {
	item := WorkItem{Type: WorkTypeFunc, Function: fn}
	select {
	case <-q.ctx.Done():
	case q.normal <- item:
	}
}

/*
Pop blocks until work is available, strictly preferring the priority lane.
*/
func (q *Queue) Pop() (WorkItem, error) {
	// 1. Non-blocking check on priority lane first
	select {
	case <-q.ctx.Done():
		return WorkItem{}, q.ctx.Err()
	case w := <-q.priority:
		return w, nil
	default:
	}

	// 2. Blocking wait on both lanes
	select {
	case <-q.ctx.Done():
		return WorkItem{}, q.ctx.Err()
	case w := <-q.priority:
		return w, nil
	case w := <-q.normal:
		return w, nil
	}
}
