package compute

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

type Pool struct {
	ctx           context.Context
	cancel        context.CancelFunc
	procs         int
	jobs          chan func(context.Context) error
	droppedErrors atomic.Uint64
	saturationCnt atomic.Uint64
	// errBufSize is the capacity of the per-Run error channel. When zero, Run uses pool.procs.
	// trySendErr is non-blocking; if this buffer fills, errors are dropped and DroppedErrors increments.
	// Raise errBufSize for workloads that may surface many failures at once.
	errBufSize int

	dropStateMu     sync.Mutex
	dropObserver    func(error)
	dropWindow      time.Duration
	dropThreshold   uint64
	dropWindowStart time.Time
	dropWindowCount uint64
}

type poolOpts func(*Pool)

func NewPool(opts ...poolOpts) (pool *Pool, err error) {
	pool = &Pool{}

	for _, opt := range opts {
		opt(pool)
	}

	if pool.dropWindow <= 0 {
		pool.dropWindow = 50 * time.Millisecond
	}
	if pool.dropThreshold == 0 {
		pool.dropThreshold = 100
	}

	if err := validate.Require(map[string]any{
		"ctx":    pool.ctx,
		"cancel": pool.cancel,
		"procs":  pool.procs,
		"jobs":   pool.jobs,
	}); err != nil {
		return nil, errnie.Error(NewPoolError(PoolErrFail, err))
	}

	if pool.procs <= 0 {
		return nil, errnie.Error(NewPoolError(PoolErrFail, errors.New("pool procs must be positive")))
	}

	return pool, nil
}

func (pool *Pool) DroppedErrors() uint64 {
	return pool.droppedErrors.Load()
}

func (pool *Pool) SaturationEvents() uint64 {
	return pool.saturationCnt.Load()
}

// trySendErr sends err to out without blocking. If out is full, the error is dropped
// and droppedErrors is incremented; increase errBufSize (PoolWithErrBuffer) if that is unacceptable.
func (pool *Pool) trySendErr(out chan error, err error) {
	select {
	case out <- err:
	default:
		droppedTotal := pool.droppedErrors.Add(1)
		pool.observeDropSaturation(droppedTotal, err)
	}
}

func (pool *Pool) observeDropSaturation(droppedTotal uint64, cause error) {
	if pool == nil || pool.dropThreshold == 0 {
		return
	}

	now := time.Now()
	var (
		saturated   bool
		windowCount uint64
		observer    func(error)
		window      time.Duration
	)

	pool.dropStateMu.Lock()
	if pool.dropWindowStart.IsZero() || now.Sub(pool.dropWindowStart) > pool.dropWindow {
		pool.dropWindowStart = now
		pool.dropWindowCount = 0
	}
	pool.dropWindowCount++
	windowCount = pool.dropWindowCount
	window = pool.dropWindow
	if pool.dropWindowCount >= pool.dropThreshold {
		pool.dropWindowStart = now
		pool.dropWindowCount = 0
		saturated = true
	}
	observer = pool.dropObserver
	pool.dropStateMu.Unlock()

	if !saturated {
		return
	}

	saturationErr := NewBackendError(BackendErrorCompleteSaturation, cause, "compute.pool.trySendErr")
	pool.saturationCnt.Add(1)
	if observer != nil {
		observer(saturationErr)
		return
	}

	errnie.Warn(
		"compute.pool.saturation",
		"err", saturationErr,
		"dropped_total", droppedTotal,
		"dropped_window", windowCount,
		"window", window.String(),
		"saturation_total", pool.saturationCnt.Load(),
	)
}

func (pool *Pool) SetDropObserver(observer func(error)) {
	if pool == nil {
		return
	}

	pool.dropStateMu.Lock()
	pool.dropObserver = observer
	pool.dropStateMu.Unlock()
}

func (pool *Pool) Run() chan error {
	bufSize := pool.errBufSize
	if bufSize <= 0 {
		bufSize = pool.procs
	}
	out := make(chan error, bufSize)
	var wg sync.WaitGroup
	for i := 0; i < pool.procs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					pool.trySendErr(out, NewPoolError(PoolErrFail, fmt.Sprintf("%v\n%s", r, debug.Stack())))
				}
			}()

			for {
				select {
				case job, ok := <-pool.jobs:
					if !ok {
						return
					}
					if job == nil {
						pool.trySendErr(out, NewPoolError(PoolErrInvalidJob, errors.New("nil job")))
						continue
					}
					if err := job(pool.ctx); err != nil {
						pool.trySendErr(out, err)
					}
				case <-pool.ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// Schedule submits job to the pool's job buffer without blocking.
// It returns nil on success. If the job buffer is full, it returns immediately with
// PoolErrFail (backpressure). If ctx is cancelled, it returns ctx.Err().
// If the pool's context is cancelled, it returns pool.ctx.Err().
func (pool *Pool) Schedule(ctx context.Context, job func(ctx context.Context) error) error {
	select {
	case pool.jobs <- job:
		return nil
	case <-pool.ctx.Done():
		return pool.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errnie.Error(NewPoolError(PoolErrFail, errors.New("job buffer full")))
	}
}

// PoolWithContext attaches ctx to the pool. Prefer calling once when constructing the pool;
// if a cancel was already installed, the previous cancel is invoked before replacing it.
func PoolWithContext(ctx context.Context) poolOpts {
	return func(pool *Pool) {
		if pool.cancel != nil {
			pool.cancel()
		}
		pool.ctx, pool.cancel = context.WithCancel(ctx)
	}
}

// PoolWithErrBuffer sets the capacity of the error channel used by Run (minimum 1).
func PoolWithErrBuffer(n int) poolOpts {
	return func(pool *Pool) {
		if n < 1 {
			n = 1
		}
		pool.errBufSize = n
	}
}

func PoolWithDropObserver(observer func(error)) poolOpts {
	return func(pool *Pool) {
		pool.dropObserver = observer
	}
}

func PoolWithDropSaturation(threshold uint64, window time.Duration) poolOpts {
	return func(pool *Pool) {
		pool.dropThreshold = threshold
		pool.dropWindow = window
	}
}

// PoolWithProcs sets worker count and, when pool.jobs is still nil, allocates a
// buffered jobs channel sized to max(pool.procs*2, 1). Use PoolWithJobBuffer
// before this option when you need a different buffer size.
func PoolWithProcs(procs int) poolOpts {
	return func(pool *Pool) {
		if procs <= 0 {
			procs = 1
		}
		pool.procs = procs
		if pool.jobs == nil {
			buf := max(pool.procs*2, 1)
			pool.jobs = make(chan func(context.Context) error, buf)
		}
	}
}

// PoolWithJobBuffer replaces pool.jobs with a new buffered channel of the given
// size (minimum 1). Call before PoolWithProcs if both are used and this should
// define the buffer.
func PoolWithJobBuffer(size int) poolOpts {
	return func(pool *Pool) {
		if size < 1 {
			size = 1
		}
		pool.jobs = make(chan func(context.Context) error, size)
	}
}

type PoolErrorType string

const (
	PoolErrFail       PoolErrorType = "pool failure"
	PoolErrInvalidJob PoolErrorType = "invalid job"
)

type PoolError struct {
	Err error
	Obj any
}

func (e *PoolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *PoolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewPoolError(err PoolErrorType, obj any) *PoolError {
	t := string(err)
	var wrapped error
	switch x := obj.(type) {
	case nil:
		wrapped = errors.New(t)
	case error:
		wrapped = fmt.Errorf("%s: %w", t, x)
	default:
		wrapped = fmt.Errorf("%s: %v", t, x)
	}
	return &PoolError{Err: wrapped, Obj: obj}
}
