//go:build !darwin || !cgo

package metal

import (
	"context"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
Backend is the stub for non-darwin builds.
*/
type Backend struct {
	idx      int
	observer kernel.Observer
}

type backendOption func(*Backend)

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx:      idx,
		observer: kernel.NoopObserver{},
	}
	for _, opt := range opts {
		opt(backend)
	}
	backend.observer = kernel.NormalizeObserver(backend.observer)
	return backend
}

// BackendWithObserver injects a kernel observer used for optional trace/error
// reporting. Pass nil to disable.
func BackendWithObserver(observer kernel.Observer) backendOption {
	return func(backend *Backend) {
		backend.observer = kernel.NormalizeObserver(observer)
	}
}

// SetObserver updates the backend observer at runtime.
func (backend *Backend) SetObserver(observer kernel.Observer) {
	backend.observer = kernel.NormalizeObserver(observer)
}

/*
Available always returns zero on non-darwin.
*/
func Available() int {
	return 0
}

func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {
	return NewMetalError(MetalErrorUnavailable, nil, "UniversalBitwise")
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func (backend *Backend) Name() string { return "metal" }
