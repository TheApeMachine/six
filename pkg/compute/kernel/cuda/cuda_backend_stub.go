//go:build !cuda || !cgo

package cuda

import (
	"bytes"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

/*
CUDABackend is the stub used on non-CUDA builds. It satisfies the
kernel.Backend interface (io.ReadWriteCloser + Available) so the
package compiles without CUDA tooling. Available probes NVML to
detect GPUs even without the CUDA compiler present.
*/
type CUDABackend struct {
	buf bytes.Buffer
}

/*
Available probes NVML for GPU count.
*/
func (backend *CUDABackend) Available() (int, error) {
	ret := nvml.Init()

	if ret != nvml.SUCCESS {
		return 0, NewCUDABackendError(CUDABackendErrorUnavailable)
	}

	defer nvml.Shutdown()

	count, ret := nvml.DeviceGetCount()

	if ret != nvml.SUCCESS {
		return 0, NewCUDABackendError(CUDABackendErrorUnavailable)
	}

	return int(count), nil
}

/*
Read drains the result buffer.
*/
func (backend *CUDABackend) Read(p []byte) (n int, err error) {
	return backend.buf.Read(p)
}

/*
Write accepts incoming data.
*/
func (backend *CUDABackend) Write(p []byte) (n int, err error) {
	return backend.buf.Write(p)
}

/*
Close resets the buffer.
*/
func (backend *CUDABackend) Close() error {
	backend.buf.Reset()
	return nil
}

/*
CUDABackendErrorType enumerates stub error kinds.
*/
type CUDABackendErrorType string

const (
	CUDABackendErrorUnavailable CUDABackendErrorType = "cuda backend unavailable"
)

/*
CUDABackendError carries a typed stub failure.
*/
type CUDABackendError struct {
	Message string
	Err     CUDABackendErrorType
}

/*
NewCUDABackendError constructs a typed error.
*/
func NewCUDABackendError(err CUDABackendErrorType) *CUDABackendError {
	return &CUDABackendError{Message: string(err), Err: err}
}

/*
Error implements error.
*/
func (err CUDABackendError) Error() string {
	return err.Message
}
