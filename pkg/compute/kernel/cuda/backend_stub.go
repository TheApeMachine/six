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

func (backend *Backend) BitwiseOr(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseAnd(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseXor(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseAndNot(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseNand(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseNor(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseXnor(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseConverseNonimplication(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) BitwiseNot(a, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) MotorApply(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) MotorInvert(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) MotorCompose(a, b, dst unsafe.Pointer, n uint32) error {
	return CUDAErrorUnavailable
}

func (backend *Backend) RollLeft(src, dst unsafe.Pointer, shift, n uint32) error {
	return CUDAErrorUnavailable
}

/*
CUDAErrorType is a typed error for CUDA backend failures.
*/
type CUDAErrorType string

const (
	CUDAErrorUnavailable    CUDAErrorType = "cuda backend unavailable"
	CUDAErrorDispatchFailed CUDAErrorType = "cuda backend dispatch failed"
)

/*
Error implements the error interface for CUDAErrorType.
*/
func (err CUDAErrorType) Error() string {
	return string(err)
}
