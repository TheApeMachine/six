package pool

import (
	"context"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Queue is the universal work scheduler. It owns the goroutine pool and
three priority-tiered lock-free ring buffers. Every subsystem that needs
to schedule work (tokenizer, compute backend, routing) receives a Queue
rather than a raw Pool — this centralizes backpressure, priority, and
spill management in one place.
*/
type Queue struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	pool     *Pool
	normal   *data.Ring
	priority *data.Ring
	spill    *data.Ring
}

/*
NewQueue constructs a Queue that owns its own goroutine pool sized to
the available CPU cores minus one (leaving the main thread free). The
optional dispatch handler is called by pool workers whenever a task
returns a non-nil Executable — this is how the compute Backend receives
work.
*/
func NewQueue(ctx context.Context, dispatch ...func(*programmer.Executable)) (*Queue, error) {
	ctx, cancel := context.WithCancel(ctx)

	queue := &Queue{
		ctx:    ctx,
		cancel: cancel,
		pool:   NewPool(uint64(runtime.NumCPU()-1), dispatch...),
	}

	queue.normal, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.priority, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.spill, queue.err = data.NewRing(ctx, data.RingCapacity)

	if queue.err != nil {
		return nil, errnie.Error(queue.err)
	}

	return queue, validate.Require(map[string]any{
		"ctx":      queue.ctx,
		"cancel":   queue.cancel,
		"pool":     queue.pool,
		"normal":   queue.normal,
		"priority": queue.priority,
		"spill":    queue.spill,
	})
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
Submit dispatches a task to the goroutine pool for immediate execution.
This is the fast path for CPU-bound work that should not queue.
*/
func (queue *Queue) Submit(task func() *programmer.Executable) {
	if queue == nil {
		return
	}

	queue.pool.Submit(task)
}

/*
Schedule enqueues work onto the normal-priority ring buffer.
Returns false when the ring is full.
*/
func (queue *Queue) Schedule(
	ctx context.Context, task func() *programmer.Executable,
) bool {
	if queue == nil {
		return false
	}

	return queue.normal.Push(unsafe.Pointer(
		&Slot{
			threadPtr: GetG(),
			task:      task,
		},
	))
}
