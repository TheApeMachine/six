package pool

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
cascadeSafetyCeiling is the hard upper bound on scheduler hops. Thermodynamic
halting (convergence, Shannon death) is the real regulator; this is only a
circuit breaker that should never fire under normal operation.
*/
const cascadeSafetyCeiling = uint32(32)

func queueWorkerCount() int {
	return max(runtime.NumCPU()-1, 1)
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
	field     *geometry.Field
	global    *geometry.Field
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
		field:  geometry.NewField(geometry.Mod8191),
		global: geometry.NewField(geometry.Mod65537),
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
publishTask avoids closure allocations in Publish and PublishTracked.
*/
type publishTask struct {
	queue    *Queue
	frame    *primitive.Value
	label    string
	tracked  bool
	hopsUsed uint32
	run      func()
	ptrs     [1]unsafe.Pointer
}

var taskPool *sync.Pool

func init() {
	taskPool = &sync.Pool{
		New: func() any {
			t := &publishTask{}
			t.run = func() { t.execute() }
			return t
		},
	}
}

func (t *publishTask) execute() error {
	queue := t.queue
	frame := t.frame
	label := t.label
	tracked := t.tracked

	// EXECUTE — run whatever program is installed on this Value.
	t.ptrs[0] = unsafe.Pointer(frame)

	if err := queue.backend.Execute(t.ptrs[:]); err != nil {
		return errnie.Error(err)
	}

	if (*frame)[kernel.SchedulingNextProgramWord] != 0 {
		hops := t.hopsUsed + 1

		if hops >= cascadeSafetyCeiling {
			(*frame)[kernel.SchedulingNextProgramWord] = 0
		} else {
			if tracked {
				queue.republishTracked(frame, label, hops)
			} else {
				// Re-enqueue without making a copy since we already own the frame
				inflight := queue.inflight.Add(1)

				if viz.DefaultBus.IsActive() {
					viz.DefaultBus.Publish(viz.QueueSubmitEvent(
						inflight,
						frame.ID(),
						(*frame)[kernel.PrevStartWord],
						(*frame)[kernel.NextStartWord],
						"",
					))
				}

				newTask := taskPool.Get().(*publishTask)
				newTask.queue = queue
				newTask.frame = frame
				newTask.label = label
				newTask.tracked = false
				newTask.hopsUsed = hops

				queue.pool.Submit(newTask.run)
			}
		}
	}

	if (*frame)[kernel.SchedulingNextProgramWord] == 0 && !tracked {
		for i := range *frame {
			(*frame)[i] = 0
		}
		publishFramePool.Put(frame)
	}

	if queue.inflight.Add(-1) == 0 {
		queue.drainMu.Lock()
		queue.drainWait.Broadcast()
		queue.drainMu.Unlock()
	}

	taskPool.Put(t)
	return nil
}

/*
republishTracked re-enqueues a tracked frame carrying the cumulative hop
counter so the safety ceiling spans the full cascade lifetime.
*/
func (queue *Queue) republishTracked(frame *primitive.Value, label string, hopsUsed uint32) {
	inflight := queue.inflight.Add(1)

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.QueueSubmitEvent(
			inflight,
			frame.ID(),
			(*frame)[kernel.PrevStartWord],
			(*frame)[kernel.NextStartWord],
			"",
		))
	}

	task := taskPool.Get().(*publishTask)
	task.queue = queue
	task.frame = frame
	task.label = label
	task.tracked = true
	task.hopsUsed = hopsUsed

	queue.pool.Submit(task.run)
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

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.QueueSubmitEvent(
			inflight,
			frame.ID(),
			(*frame)[kernel.PrevStartWord],
			(*frame)[kernel.NextStartWord],
			"",
		))
	}

	task := taskPool.Get().(*publishTask)
	task.queue = queue
	task.frame = frame
	task.label = label
	task.tracked = false
	task.hopsUsed = 0

	queue.pool.Submit(task.run)

	return nil
}

/*
PublishTracked enqueues a task to the normal-priority ring buffer WITHOUT
making a copy. The caller retains ownership of the Value and MUST NOT close
it until the queue drains. Cascade halting is driven by thermodynamics
(convergence / Shannon death); the safety ceiling lives on the task counter.
*/
func (queue *Queue) PublishTracked(frame *primitive.Value, label string) error {
	if queue == nil {
		return errors.New("queue: nil")
	}

	if queue.backend == nil {
		return errors.New("queue: no backend")
	}

	if frame == nil {
		return errors.New("queue: nil frame")
	}

	inflight := queue.inflight.Add(1)

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.QueueSubmitEvent(
			inflight,
			frame.ID(),
			(*frame)[kernel.PrevStartWord],
			(*frame)[kernel.NextStartWord],
			"",
		))
	}

	task := taskPool.Get().(*publishTask)
	task.queue = queue
	task.frame = frame
	task.label = label
	task.tracked = true
	task.hopsUsed = 0

	queue.pool.Submit(task.run)

	return nil
}

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
	defer queue.drainMu.Unlock()

	for queue.inflight.Load() > 0 {
		queue.drainWait.Wait()
	}
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
