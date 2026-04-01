package compute

import (
	"context"
	"time"
)

/*
NewBackgroundBackend returns NewBackend(context.Background(), opts...).
*/
func NewBackgroundBackend(opts ...BackendOption) *Backend {
	return NewBackend(context.Background(), opts...)
}

/*
WithContext sets the context for the backend.
*/
func WithContext(ctx context.Context) BackendOption {
	return func(backend *Backend) {
		if ctx == nil {
			ctx = context.Background()
		}
		backend.ctx, backend.cancel = context.WithCancel(ctx)
	}
}

/*
WithPool injects the worker pool for job scheduling.
*/
func WithPool(p *Pool) BackendOption {
	return func(backend *Backend) {
		backend.pool = p
	}
}

/*
WithBatchSize sets the maximum number of frames folded together in one
batched hardware dispatch.
*/
func WithBatchSize(n int) BackendOption {
	return func(backend *Backend) {
		if n > 0 {
			backend.batchSize = n
		}
	}
}

/*
WithBatchWindow sets the maximum time the gather loop will wait for more
work before dispatching the current batch.
*/
func WithBatchWindow(window time.Duration) BackendOption {
	return func(backend *Backend) {
		if window >= 0 {
			backend.batchWindow = window
		}
	}
}
