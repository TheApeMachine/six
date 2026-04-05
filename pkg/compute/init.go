package compute

import (
	"context"
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
