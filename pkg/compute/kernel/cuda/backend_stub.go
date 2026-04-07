//go:build !cuda || !cgo

package cuda

import (
	"context"
	"unsafe"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
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
	observer  kernel.Observer
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
		observer:  kernel.NoopObserver{},
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

func (backend *Backend) UniversalBitwise(frames []unsafe.Pointer) error {
	return NewCUDAKernelError(kernel.KernelErrUnavailable, nil, "UniversalBitwise", 0)
}

func (backend *Backend) BatchDistances(
	query unsafe.Pointer, candidates unsafe.Pointer, count int, distances []uint32,
) error {
	return NewCUDAKernelError(kernel.KernelErrUnavailable, nil, "BatchDistances", 0)
}

func (backend *Backend) NearestAffinity(
	query unsafe.Pointer, candidates unsafe.Pointer, count int,
) ([]uint32, error) {
	return nil, NewCUDAKernelError(kernel.KernelErrUnavailable, nil, "NearestAffinity", 0)
}

// Schedule runs the job with Context(); cancellation is tied to Shutdown.
func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(backend.ctx)
}

func (backend *Backend) Name() string { return "cuda" }
