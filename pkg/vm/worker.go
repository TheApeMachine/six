package vm

import "context"

type Worker struct {
	job func(ctx context.Context) error
}

func NewWorker(job func(ctx context.Context) error) *Worker {
	return &Worker{
		job: job,
	}
}

func (worker *Worker) Run(ctx context.Context) error {
	defer workerPool.Put(worker)

	defer func() {
		if r := recover(); r != nil {
			panic(NewPoolError(PoolErrFail, r))
		}
	}()

	return worker.job(ctx)
}
