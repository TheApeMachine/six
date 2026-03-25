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

/*
CUDAErrorType is a typed error for CUDA backend failures.
*/
type CUDAErrorType string

const (
	CUDAErrorUnavailable    CUDAErrorType = "cuda backend unavailable"
	CUDAErrorDispatchFailed CUDAErrorType = "cuda backend dispatch failed"
)

type CUDAError struct {
	Err error
	Msg string
	Op  string
	N   uint32
}

/*
NewCUDAError returns a new CUDAError.
*/
func NewCUDAError(err error, msg string, op string, n uint32) *CUDAError {
	return &CUDAError{
		Err: err,
		Msg: msg,
		Op:  op,
		N:   n,
	}
}

/*
Error implements the error interface for CUDAErrorType.
*/
func (err *CUDAError) Error() string {
	return err.Msg
}
