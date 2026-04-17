package pool

import (
	"context"
	"io"
	"runtime"

	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
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

	/*
		stream is a dedicated byte ring for io.ReadWriter. Task slots on
		normal, priority, and spill carry *Slot pointers; mixing raw frame
		bytes on those rings would corrupt the scheduler, so I/O uses its
		own Vyukov queue at the same capacity as the task tiers.
	*/
	stream *data.Ring
}

/*
NewQueue constructs a Queue that owns its own goroutine pool sized to
the available CPU cores minus one (leaving the main thread free). The
optional dispatch handler is called by pool workers whenever a task
returns a non-nil Executable — this is how the compute Backend receives
work.
*/
func NewQueue(ctx context.Context, dispatch ...func(*primitive.Value)) (*Queue, error) {
	ctx, cancel := context.WithCancel(ctx)

	queue := &Queue{
		ctx:    ctx,
		cancel: cancel,
		pool:   NewPool(uint64(runtime.NumCPU()-1), dispatch...),
	}

	queue.normal, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.priority, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.spill, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.stream, queue.err = data.NewRing(ctx, data.RingCapacity)

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
		"stream":   queue.stream,
	})
}

/*
Len returns the aggregate number of slots waiting across normal, priority,
and spill rings. Used for pipeline quiescence.
*/
func (queue *Queue) Len() int {
	if queue == nil {
		return 0
	}

	return queue.normal.Len() + queue.priority.Len() + queue.spill.Len()
}

/*
Read implements io.Reader by dequeuing byte frames from the dedicated
stream ring (see Queue.stream).
*/
func (queue *Queue) Read(p []byte) (n int, err error) {
	if queue == nil || queue.stream == nil {
		return 0, io.ErrClosedPipe
	}

	return queue.stream.Read(p)
}

/*
Write implements io.Writer by enqueueing byte frames on the dedicated
stream ring.
*/
func (queue *Queue) Write(p []byte) (n int, err error) {
	if queue == nil || queue.stream == nil {
		return 0, io.ErrClosedPipe
	}

	return queue.stream.Write(p)
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

var _ io.ReadWriteCloser = (*Queue)(nil)

/*
Submit dispatches a Value to the goroutine pool as a task to be
executed by the ALU. Priority queue should be normal priority by
default, but could be overwritten using a new word in the Value's
Properties region.
*/
func (queue *Queue) Submit(value *primitive.Value) {
	if queue == nil {
		return
	}

	queue.pool.Submit(value)
}
