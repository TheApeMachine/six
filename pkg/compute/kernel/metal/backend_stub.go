//go:build !darwin || !cgo

package metal

import (
	"context"
	"unsafe"
)

/*
Backend is the stub for non-darwin builds.
*/
type Backend struct {
	idx int
}

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		idx: idx,
	}
}

/*
Available always returns zero on non-darwin.
*/
func Available() int {
	return 0
}

func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	return NewMetalError(MetalErrorUnavailable, nil, "UniversalBitwise")
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}
