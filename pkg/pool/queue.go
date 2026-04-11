package pool

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

func queueWorkerCount() int {
	n := runtime.NumCPU() - 1

	if n < 1 {
		n = 1
	}

	return n
}

/*
QueueBackend runs pre-layout ALU work: the Value must already carry the
program bits (and operands) the substrate expects; dispatch is opcode-driven.
*/
type QueueBackend interface {
	Execute(frames []unsafe.Pointer) error
}

/*
Queue is the universal work scheduler. It owns the goroutine pool and
three priority-tiered lock-free ring buffers. Every subsystem that needs
to schedule work (tokenizer, compute backend, routing) receives a Queue
rather than a raw Pool — this centralizes backpressure, priority, and
spill management in one place.
*/
type Queue struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	pool      *Pool
	backend   QueueBackend
	normal    *data.Ring
	priority  *data.Ring
	spill     *data.Ring
	inflight  atomic.Int64
	drainMu   sync.Mutex
	drainWait *sync.Cond
}

/*
publishFramePool gives Queue.Publish ownership of the frame handed to backend
workers; tokenizers may close and recycle the original Value before async ALU
work runs.
*/
var publishFramePool = sync.Pool{
	New: func() any {
		return new(primitive.Value)
	},
}

/*
NewQueue constructs a Queue that owns its own goroutine pool sized to
the available CPU cores minus one (leaving the main thread free).
*/
func NewQueue(ctx context.Context) (*Queue, error) {
	ctx, cancel := context.WithCancel(ctx)

	queue := &Queue{
		ctx:    ctx,
		cancel: cancel,
		pool:   NewPool(uint64(queueWorkerCount())),
	}

	queue.normal, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.priority, queue.err = data.NewRing(ctx, data.RingCapacity)
	queue.spill, queue.err = data.NewRing(ctx, data.RingCapacity)

	if queue.err != nil {
		return nil, errnie.Error(queue.err)
	}

	queue.drainWait = sync.NewCond(&queue.drainMu)

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
Publish enqueues a task to the normal-priority ring buffer.
It implement the Publishable interface.
*/
func (queue *Queue) Publish(value *primitive.Value, label string) error {
	if queue == nil {
		return errors.New("queue: nil")
	}

	if queue.backend == nil {
		return errors.New("queue: no backend")
	}

	frame := publishFramePool.Get().(*primitive.Value)

	if value == nil {
		*frame = primitive.Value{}
	} else {
		*frame = *value
	}

	inflight := queue.inflight.Add(1)
	viz.DefaultBus.Publish(viz.QueueSubmitEvent(inflight))

	queue.pool.Submit(func() {
		defer func() {
			*frame = primitive.Value{}
			publishFramePool.Put(frame)

			if queue.inflight.Add(-1) == 0 {
				queue.drainMu.Lock()
				queue.drainWait.Broadcast()
				queue.drainMu.Unlock()
			}
		}()

		_ = label
		_ = queue.backend.Execute([]unsafe.Pointer{unsafe.Pointer(frame)})
	})

	return nil
}

/*
Submit dispatches a task to the goroutine pool for immediate execution.
This is the fast path for CPU-bound work that should not queue.
*/
func (queue *Queue) Submit(task func()) {
	if queue == nil {
		return
	}

	queue.pool.Submit(task)
}

/*
SubmitTracked runs task on the pool and includes it in inflight so
Drain waits for completion — same lifecycle as Execute, without a
compute backend.
*/
func (queue *Queue) SubmitTracked(task func()) {
	if queue == nil || task == nil {
		return
	}

	queue.inflight.Add(1)

	queue.pool.Submit(func() {
		defer func() {
			if queue.inflight.Add(-1) == 0 {
				queue.drainMu.Lock()
				queue.drainWait.Broadcast()
				queue.drainMu.Unlock()
			}
		}()

		task()
	})
}

/*
SetBackend wires the compute backend into the queue for Publish.
*/
func (queue *Queue) SetBackend(backend QueueBackend) {
	queue.backend = backend
}

/*
Drain spins until all inflight Execute and SubmitTracked tasks have
completed. This lets callers ensure prior GPU dispatches and pooled
side effects finish before triggering work that would contend on the
same resources.
*/
func (queue *Queue) Drain() {
	if queue == nil {
		return
	}

	queue.drainMu.Lock()

	for queue.inflight.Load() > 0 {
		queue.drainWait.Wait()
	}

	queue.drainMu.Unlock()
}

/*
Schedule enqueues work onto the normal-priority ring buffer.
Returns false when the ring is full.
*/
func (queue *Queue) Schedule(
	ctx context.Context, task func(),
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
