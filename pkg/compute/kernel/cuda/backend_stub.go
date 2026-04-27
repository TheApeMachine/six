//go:build !cuda || !cgo

package cuda

import (
	"context"
	"errors"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

// ErrUnavailable is returned when the CUDA backend is not initialised.
var ErrUnavailable = errors.New("cuda: backend unavailable")

/*
Backend is the stub used on non-CUDA builds. It satisfies the full
kernel.Substrate contract by delegating every call to the cross-
substrate CPU helpers.
*/
type Backend struct {
	deviceIdx int
	ctx       context.Context
	cancel    context.CancelFunc
}

type backendOption func(*Backend)

/*
NewBackend returns a stub Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	backend := &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
	}
	for _, opt := range opts {
		opt(backend)
	}
	return backend
}

func (backend *Backend) Close() error {
	if backend.cancel != nil {
		backend.cancel()
	}
	return nil
}

/*
Available always returns 0 on non-CUDA builds.
*/
func Available() int { return 0 }

func (backend *Backend) Name() string { return "cuda" }

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) ([]*primitive.Value, []kernel.StageRequest, error) {
	return nil, nil, nil
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return false
}
