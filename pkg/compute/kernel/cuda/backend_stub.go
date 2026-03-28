//go:build !cuda || !cgo

package cuda

import (
	"context"
	"unsafe"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
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

/*
NewBackend returns a stub Backend.
*/
func NewBackend(idx int) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	return &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Context returns the backend-scoped context canceled by Shutdown.
func (backend *Backend) Context() context.Context {
	return backend.ctx
}

// Shutdown cancels the backend context used by Schedule.
func (backend *Backend) Shutdown() {
	if backend.cancel != nil {
		backend.cancel()
	}
}

/*
Available probes NVML for GPU count.
*/
func Available() int {
	ret := nvml.Init()

	if ret != nvml.SUCCESS {
		return 0
	}

	defer nvml.Shutdown()

	count, ret := nvml.DeviceGetCount()

	if ret != nvml.SUCCESS {
		return 0
	}

	return int(count)
}

func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	return NewCUDAError(CUDAErrorUnavailable, nil, "UniversalBitwise", 0)
}

// Schedule runs the job with Context(); cancellation is tied to Shutdown.
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(backend.ctx)
}
