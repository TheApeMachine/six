//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int universal_bitwise_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t op, uint32_t n);
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
}

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend() *Backend {
	return &Backend{}
}

func (backend *Backend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())

		if backend.deviceCount < 0 {
			backend.deviceCount = 0
		}
	})
}

/*
Available returns the number of CUDA-capable GPUs.
*/
func Available() int {
	backend := &Backend{}
	backend.init()
	return backend.deviceCount
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

// UniversalBitwise reads the 4-bit opcode from each Value's instruction
// region and dispatches to the compiled CUDA kernel.
func (backend *Backend) UniversalBitwise(a, bPtr, dst unsafe.Pointer, n uint32) error {
	// Read the opcode from the first Value's instruction region.
	as := unsafe.Slice((*primitive.Value)(a), n)
	word := primitive.InstrStart >> 6
	shift := uint(primitive.InstrStart & 63)
	mask := uint64((1 << primitive.InstrBits) - 1)
	op := uint32((as[0][word] >> shift) & mask)

	if C.universal_bitwise_cuda(0, a, bPtr, dst, C.uint32_t(op), C.uint32_t(n)) != 0 {
		return NewCUDAError(fmt.Errorf("dispatch failed for op=%d", op), string(CUDAErrorDispatchFailed), "UniversalBitwise", n)
	}
	return nil
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
