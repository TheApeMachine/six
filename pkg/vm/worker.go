package vm

import (
	"context"
	"errors"
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
	var w *Worker
	if v := workerPool.Get(); v != nil {
		w = v.(*Worker)
	} else {
		w = &Worker{}
	}
	w.job = job
	return w
}

func (worker *Worker) Run(ctx context.Context) (err error) {
	if worker.job == nil {
		return NewPoolError(PoolErrInvalidJob, errors.New("nil job"))
	}
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
