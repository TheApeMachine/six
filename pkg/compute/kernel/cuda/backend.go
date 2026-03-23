//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int bitwise_or_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_and_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_xor_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_and_not_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_nand_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_nor_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_xnor_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_converse_nonimplication_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int bitwise_not_cuda(int device_id, const void* a, void* dst, uint32_t n);

int motor_apply_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int motor_invert_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
int motor_compose_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);

int roll_left_cuda(int device_id, const void* src, void* dst, uint32_t shift, uint32_t n);
*/
import "C"
import (
	"sync"
	"unsafe"
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
	backend.init()

	if backend.deviceCount == 0 {
		return 0
	}

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

func (backend *Backend) BitwiseOr(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_or_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseAnd(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_and_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseXor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_xor_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseAndNot(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_and_not_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseNand(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_nand_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseNor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_nor_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseXnor(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_xnor_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseConverseNonimplication(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_converse_nonimplication_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) BitwiseNot(a, dst unsafe.Pointer, numValues uint32) error {
	if C.bitwise_not_cuda(0, a, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) MotorApply(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.motor_apply_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) MotorInvert(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.motor_invert_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) MotorCompose(a, b, dst unsafe.Pointer, numValues uint32) error {
	if C.motor_compose_cuda(0, a, b, dst, C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
	}

	return nil
}

func (backend *Backend) RollLeft(src, dst unsafe.Pointer, shift, numValues uint32) error {
	if C.roll_left_cuda(0, src, dst, C.uint32_t(shift), C.uint32_t(numValues)) != 0 {
		return CUDAErrorDispatchFailed
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

/*
Error implements the error interface for CUDAErrorType.
*/
func (err CUDAErrorType) Error() string {
	return string(err)
}
