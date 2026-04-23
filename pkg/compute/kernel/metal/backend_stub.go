//go:build !darwin || !cgo

package metal

import "github.com/theapemachine/six/pkg/compute/kernel"

/*
Backend is the stub for non-darwin builds.
*/
type Backend struct {
	idx int
}

type backendOption func(*Backend)

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx: idx,
	}
	for _, opt := range opts {
		opt(backend)
	}
	return backend
}

/*
Available always returns zero on non-darwin.
*/
func Available() int {
	return 0
}

func (backend *Backend) UniversalBitwise(optimizer *kernel.Optimizer) { return }

func (backend *Backend) Name() string { return "metal" }
