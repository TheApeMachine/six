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
}

/*
NewBackend returns a stub Backend.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		deviceIdx: idx,
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
	return NewCUDAError(CUDAErrorUnavailable, nil, "UniversalBitwise")
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	_ = job(context.Background())
}
