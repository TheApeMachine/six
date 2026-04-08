package pool

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

func queueWorkerCount() int {
	n := runtime.NumCPU() - 1

	if n < 1 {
		n = 1
	}

	return n
}

/*
Executor is the interface that the compute backend satisfies. It
allows the Queue to dispatch compiled programs without importing
the compute package (which already imports pool).
*/
type Executor interface {
	CompileAndExecute(program any) error
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
	backend   Executor
	normal    *data.Ring
	priority  *data.Ring
	spill     *data.Ring
	inflight  atomic.Int64
	drainMu   sync.Mutex
	drainWait *sync.Cond
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
SetBackend wires the compute backend into the queue so Execute
can defer compilation to the moment a substrate is picked.
*/
func (queue *Queue) SetBackend(backend Executor) {
	queue.backend = backend
}

/*
Execute submits an uncompiled program to the pool. The backend
picks the best hardware substrate, compiles for that target, and
executes — all inside a pooled goroutine.
*/
func (queue *Queue) Execute(program any) {
	if queue == nil || queue.backend == nil {
		return
	}

	queue.inflight.Add(1)

	backend := queue.backend
	queue.pool.Submit(func() {
		defer func() {
			if queue.inflight.Add(-1) == 0 {
				queue.drainMu.Lock()
				queue.drainWait.Broadcast()
				queue.drainMu.Unlock()
			}
		}()

		backend.CompileAndExecute(program)
	})
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
ExecuteSync compiles and executes a program in the calling goroutine,
blocking until completion. Use this when the caller needs to read
results from the Value immediately after execution — typically inside
a Submit callback that is already running on a pooled goroutine.
*/
func (queue *Queue) ExecuteSync(program any) error {
	if queue == nil || queue.backend == nil {
		return nil
	}

	return queue.backend.CompileAndExecute(program)
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
