package compute

import (
	"context"
)

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
