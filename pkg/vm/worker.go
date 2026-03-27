package vm

import (
	"context"
	"sync"
)

// workerPool reuses Worker shells allocated by NewWorker.
var workerPool = sync.Pool{
	New: func() any {
		return &Worker{}
	},
}

type Worker struct {
	job func(ctx context.Context) error
}

func NewWorker(job func(ctx context.Context) error) *Worker {
	return &Worker{
		job: job,
	}
}

func (worker *Worker) Run(ctx context.Context) (err error) {
	defer func() {
		worker.job = nil
		workerPool.Put(worker)
	}()

	defer func() {
		if r := recover(); r != nil {
			err = NewPoolError(PoolErrFail, r)
		}
	}()

	return worker.job(ctx)
}
