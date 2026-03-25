//go:build !cuda || !cgo

package cuda

import (
	"unsafe"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

/*
Backend is the stub used on non-CUDA builds. Available probes NVML to
detect GPUs even without the CUDA compiler present.
*/
type Backend struct{}

/*
NewBackend returns a stub Backend.
*/
func NewBackend() *Backend {
	return &Backend{}
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

func (backend *Backend) Read(p []byte) (n int, err error) {
	return
}

func (backend *Backend) Write(p []byte) (n int, err error) {
	return
}

func (backend *Backend) Close() error {
	return nil
}

func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	return NewCUDAError(nil, "UniversalBitwise", "UniversalBitwise", n)
}

// CUDAError, NewCUDAError, and related constants are defined in errors.go
// (no build tag) so both the CUDA and stub builds share one implementation.
