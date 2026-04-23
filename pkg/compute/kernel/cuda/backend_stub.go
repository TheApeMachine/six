//go:build !cuda || !cgo

package cuda

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

/*
Backend is the stub used on non-CUDA builds. Available probes NVML to
detect GPUs even without the CUDA compiler present.
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

func (backend *Backend) Close() {
	if backend.cancel != nil {
		backend.cancel()
	}
}

/*
Available probes NVML for GPU count.
*/
func Available() int {
	return 0
}

func (backend *Backend) UniversalBitwise(optimizer *kernel.Optimizer) { return }

func (backend *Backend) Name() string { return "cuda" }
