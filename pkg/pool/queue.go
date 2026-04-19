package pool

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

// emitBufPool reuses per-frame buffers across the emit hook fast path.
// wrappedDispatch runs concurrently from every pool worker; sync.Pool is
// the right fit (a per-Queue buffer would need a mutex and serialize
// every emit). The pooled []byte is sized to core.Cfg.Value.Bytes once
// at first Get and reused for the lifetime of the process.
var emitBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, core.Cfg.Value.Bytes)
		return &b
	},
}

/*
Queue is the scheduler and byte-stream facade: goroutine pool, priority
rings, and a dedicated stream ring for io-compatible frames. The only
implementation is the concrete type constructed by NewQueue; callers may
depend on this interface for tests and alternate backends without importing
unexported types.
*/
type Scheduler interface {
	io.ReadWriteCloser
	Submit(value *primitive.Value)
	Schedule(fn func())
	Len() int
	Error() error
}

/*
queue is the universal work scheduler. It owns the goroutine pool and
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

	/*
		emitHook is invoked by the wrapped dispatch (see NewQueue) AFTER
		the user's Dispatch returns, but only when the post-ALU Value
		raised its EmitRequested flag. It is the bridge from the compute
		substrate back into gossip.Conn's outbound ring, with no Pool
		modification required.
	*/
	emitHook atomic.Pointer[func([]byte)]

	/*
		inflight tracks tasks the Queue believes are still in-flight on
		the pool: incremented on Submit/Schedule, decremented when the
		wrapped dispatch / wrapped fn returns. Combined with the ring
		Lens, this gives the Orchestrator a cheap quiescence read.
	*/
	inflight atomic.Int64
}

/*
NewQueue constructs a Queue that owns its own goroutine pool sized to
the available CPU cores minus one (leaving the main thread free). The
optional dispatch handler is called by pool workers whenever a task
returns a non-nil Executable — this is how the compute Backend receives
work.

The wrapped dispatch handles three things in one place:
  - decrements the in-flight counter so quiescence detection works,
  - calls the caller-supplied Dispatch (compute.Backend.Dispatch in production),
  - if the post-ALU Value has its EMIT property raised, ships the wire
    frame to the registered emit hook (set by the orchestrator to
    gossip.Conn.Emit). Without this hook the post-ALU frame would die
    on the worker stack.
*/
func NewQueue(
	ctx context.Context, dispatch ...func(*primitive.Value),
) (q *Queue, err error) {
	ctx, cancel := context.WithCancel(ctx)

	q = &Queue{
		ctx:    ctx,
		cancel: cancel,
	}

	var userDispatch func(*primitive.Value)

	if len(dispatch) > 0 {
		userDispatch = dispatch[0]
	}

	wrappedDispatch := func(value *primitive.Value) {
		defer q.inflight.Add(-1)

		if userDispatch != nil {
			userDispatch(value)
		}

		if value == nil {
			return
		}

		if !value.EmitRequested() {
			return
		}

		hookPtr := q.emitHook.Load()

		if hookPtr == nil {
			return
		}

		bufPtr := emitBufPool.Get().(*[]byte)
		buf := (*bufPtr)[:core.Cfg.Value.Bytes]

		// Value.Read returns (Bytes, io.EOF) on success — the EOF is the
		// per-frame delimiter, not an error condition.
		if _, rerr := value.Read(buf); rerr != nil && rerr != io.EOF {
			emitBufPool.Put(bufPtr)
			return
		}

		(*hookPtr)(buf)
		emitBufPool.Put(bufPtr)
	}

	q.pool = NewPool(uint64(runtime.NumCPU()-1), wrappedDispatch)

	q.normal, err = data.NewRing(ctx, data.RingCapacity)
	errors.Join(err, q.err)
	q.priority, q.err = data.NewRing(ctx, data.RingCapacity)
	errors.Join(err, q.err)
	q.spill, q.err = data.NewRing(ctx, data.RingCapacity)
	errors.Join(err, q.err)
	q.stream, q.err = data.NewRing(ctx, data.RingCapacity)
	errors.Join(err, q.err)

	if q.err != nil {
		return nil, errnie.Error(q.err)
	}

	if err := validate.Require(map[string]any{
		"ctx":      q.ctx,
		"cancel":   q.cancel,
		"pool":     q.pool,
		"normal":   q.normal,
		"priority": q.priority,
		"spill":    q.spill,
		"stream":   q.stream,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	return q, nil
}

/*
Len returns the aggregate number of slots waiting across normal, priority,
and spill rings, plus the count of tasks the pool has accepted but not
yet finished. Orchestrator quiescence relies on this being a true zero
when nothing is pending or executing.
*/
func (queue *Queue) Len() int {
	if queue == nil {
		return 0
	}

	return queue.normal.Len() +
		queue.priority.Len() +
		queue.spill.Len() +
		int(queue.inflight.Load())
}

/*
SetEmitHook installs the post-dispatch callback. The hook receives the
post-ALU wire frame whenever a Value has Value.EmitRequested set. Pure
write of an atomic pointer — safe to call before or after Submit.
*/
func (queue *Queue) SetEmitHook(hook func([]byte)) {
	if queue == nil {
		return
	}

	if hook == nil {
		queue.emitHook.Store(nil)

		return
	}

	queue.emitHook.Store(&hook)
}

/*
Read implements io.Reader by dequeuing byte frames from the dedicated
stream ring (see queue.stream).
*/
func (queue *Queue) Read(p []byte) (n int, err error) {
	if queue == nil || queue.stream == nil {
		return 0, io.ErrClosedPipe
	}

	return queue.stream.Read(p)
}

/*
Write implements io.Writer by enqueueing byte frames on the dedicated
stream ring. This path is the byte-stream face of the Queue and is
intentionally distinct from Submit, which decodes a wire frame into a
*primitive.Value before handing it to the pool.
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
	if queue == nil {
		return nil
	}

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
Submit dispatches a Value to the goroutine pool as a task to be
executed by the ALU. Priority queue should be normal priority by
default, but could be overwritten with a new word in the Value's
Properties region.

Inflight is bumped here and decremented by the wrapped dispatch in
NewQueue so Len() reports a true zero when the system has gone quiet.
*/
func (queue *Queue) Submit(value *primitive.Value) {
	if queue == nil {
		return
	}

	queue.inflight.Add(1)
	queue.pool.Submit(value)
}

/*
Schedule a function to be executed by the goroutine pool. The closure
is wrapped so the inflight counter is decremented when fn returns —
without that wrap, Schedule'd work would never count toward quiescence.
*/
func (queue *Queue) Schedule(fn func()) {
	if queue == nil || fn == nil {
		return
	}

	queue.inflight.Add(1)

	queue.pool.Schedule(func() {
		defer queue.inflight.Add(-1)

		fn()
	})
}
