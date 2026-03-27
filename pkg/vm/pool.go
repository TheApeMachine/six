package vm

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

type Pool struct {
	ctx           context.Context
	cancel        context.CancelFunc
	procs         int
	jobs          chan func(context.Context) error
	droppedErrors atomic.Uint64
}

type poolOpts func(*Pool)

func NewPool(opts ...poolOpts) (pool *Pool, err error) {
	pool = &Pool{}

	for _, opt := range opts {
		opt(pool)
	}

	if err := validate.Require(map[string]any{
		"ctx":    pool.ctx,
		"cancel": pool.cancel,
		"procs":  pool.procs,
		"jobs":   pool.jobs,
	}); err != nil {
		return nil, errnie.Error(NewPoolError(PoolErrFail, err))
	}

	return pool, nil
}

func (pool *Pool) DroppedErrors() uint64 {
	return pool.droppedErrors.Load()
}

func (pool *Pool) trySendErr(out chan error, err error) {
	select {
	case out <- err:
	default:
		pool.droppedErrors.Add(1)
	}
}

func (pool *Pool) Run() chan error {
	out := make(chan error, pool.procs)

	for range pool.procs {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					pool.trySendErr(out, NewPoolError(PoolErrFail, r))
				}
			}()

			for {
				select {
				case job := <-pool.jobs:
					if err := job(pool.ctx); err != nil {
						pool.trySendErr(out, err)
					}
				case <-pool.ctx.Done():
					return
				}
			}
		}()
	}

	return out
}

func (pool *Pool) Schedule(ctx context.Context, job func(ctx context.Context) error) error {
	select {
	case pool.jobs <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errnie.Error(NewPoolError(PoolErrFail, errors.New("job buffer full")))
	}
}

func PoolWithContext(ctx context.Context) poolOpts {
	return func(pool *Pool) {
		pool.ctx, pool.cancel = context.WithCancel(ctx)
	}
}

// PoolWithProcs sets worker count and, when pool.jobs is still nil, allocates a
// buffered jobs channel sized to max(pool.procs*2, 1). Use PoolWithJobBuffer
// before this option when you need a different buffer size.
func PoolWithProcs(procs int) poolOpts {
	return func(pool *Pool) {
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
	PoolErrFail PoolErrorType = "pool failure"
)

type PoolError struct {
	Err error
	Msg string
	Obj any
}

func (e *PoolError) Error() string {
	return e.Msg
}

func NewPoolError(err PoolErrorType, obj any) *PoolError {
	return &PoolError{
		Msg: string(err),
		Err: errors.New(string(err)),
		Obj: obj,
	}
}
