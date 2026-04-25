package compute

import (
	"context"
)

/*
Queue is the universal work scheduler. It keeps normal work separate from
priority work so latency-sensitive optimizer jobs can run before bulk tasks.
*/
type Queue struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	normal   chan func()
	priority chan func()
}

/*
NewQueue constructs a Queue with bounded normal and priority buffers.
*/
func NewQueue(ctx context.Context) (*Queue, error) {
	ctx, cancel := context.WithCancel(ctx)

	queue := &Queue{
		ctx:      ctx,
		cancel:   cancel,
		normal:   make(chan func(), 1024),
		priority: make(chan func(), 1024),
	}

	return queue, nil
}

/*
Close cancels the queue context.
*/
func (queue *Queue) Close() error {
	queue.cancel()
	return queue.err
}

/*
Error returns the queue error.
*/
func (queue *Queue) Error() error {
	return queue.err
}

/*
Schedule enqueues work onto the normal-priority ring buffer.
Returns false when the ring is full.
*/
func (queue *Queue) Schedule(
	ctx context.Context, task func(),
) bool {
	if queue == nil || task == nil {
		return false
	}

	select {
	case <-queue.ctx.Done():
		return false
	case <-ctx.Done():
		return false
	case queue.normal <- task:
		return true
	default:
		return false
	}
}

/*
SchedulePriority enqueues work ahead of normal-priority jobs.
*/
func (queue *Queue) SchedulePriority(
	ctx context.Context, task func(),
) bool {
	if queue == nil || task == nil {
		return false
	}

	select {
	case <-queue.ctx.Done():
		return false
	case <-ctx.Done():
		return false
	case queue.priority <- task:
		return true
	default:
		return false
	}
}

/*
Next returns the next task to be executed.
*/
func (queue *Queue) Next() func() {
	select {
	case task := <-queue.priority:
		return task
	default:
	}

	select {
	case task := <-queue.priority:
		return task
	case task := <-queue.normal:
		return task
	case <-queue.ctx.Done():
		return func() {}
	}
}
