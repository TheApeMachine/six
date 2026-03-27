package vm

import (
	"context"
	"errors"
	"sync"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

var workerPool = sync.Pool{
	New: func() any {
		return &Worker{}
	},
}

type Pool struct {
	ctx    context.Context
	cancel context.CancelFunc
	procs  int
	jobs   chan func(context.Context) error
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

func (pool *Pool) Run() chan error {
	out := make(chan error, pool.procs)

	for range pool.procs {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					out <- NewPoolError(PoolErrFail, r)
				}
			}()

			for {
				select {
				case job := <-pool.jobs:
					if err := job(pool.ctx); err != nil {
						out <- err
					}
				case <-pool.ctx.Done():
					return
				}
			}
		}()
	}

	return out
}

func (pool *Pool) Schedule(job func(ctx context.Context) error) {
	pool.jobs <- job
}

func PoolWithContext(ctx context.Context) poolOpts {
	return func(pool *Pool) {
		pool.ctx, pool.cancel = context.WithCancel(ctx)
	}
}

func PoolWithProcs(procs int) poolOpts {
	return func(pool *Pool) {
		pool.procs = procs
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
