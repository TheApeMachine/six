//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, const void* a, const void* b, void* dst, uint32_t n);
*/
import "C"
import (
	"context"
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
	deviceIdx   int
}

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend(idx int) *Backend {
	return &Backend{
		deviceIdx: idx,
	}
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

/*
UniversalBitwise dispatches a batch of Values to the compiled CUDA kernel.

The opcode is no longer passed explicitly — each Value carries its own
64-op program in Region 3 (bits 4352–4607). The unified_bitwise_kernel
reads that program and executes up to 64 ticks per Value, halting at the
first zero opcode. The batch may therefore be heterogeneous: each Value
runs its own independent program in parallel.
*/
func (backend *Backend) UniversalBitwise(a, bPtr, dst unsafe.Pointer, n uint32) error {
	if C.unified_bitwise_cuda(0, a, bPtr, dst, C.uint32_t(n)) != 0 {
		return NewCUDAError(
			CUDAErrorDispatchFailed,
			"failed to dispatch unified bitwise operation",
			"UniversalBitwise",
			n,
		)
	}

	return nil
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	_ = job(context.Background())
}
