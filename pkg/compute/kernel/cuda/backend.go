//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int unified_bitwise_cuda(int device_id, void* a_host, uint32_t num_values);
int nearest_affinity_cuda(int device_id, void* query_host, void* candidates_host, uint32_t count, uint64_t* best_packed_result_host);
*/
import "C"
import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
	ctx         context.Context
	cancel      context.CancelFunc
}

type backendOption func(*Backend)

/*
NewBackend returns a CUDA kernel Backend.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	backend := &Backend{
		deviceIdx: idx,
		ctx:       ctx,
		cancel:    cancel,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func (backend *Backend) Name() string { return "cuda" }

func (backend *Backend) Close() {
	if backend.cancel != nil {
		backend.cancel()
	}

	C.cleanup_cuda_pools()
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
	b := NewBackend(0)
	b.init()

	return b.deviceCount
}

/*
UniversalBitwise runs the unified bitwise kernel on each Value frame.
*/
func (backend *Backend) UniversalBitwise(optimizer *kernel.Optimizer) {
	if C.unified_bitwise_cuda(
		C.int(backend.deviceIdx),
		ptr,
		C.uint32_t(1),
	) != 0 {
		if C.unified_bitwise_cuda(
			C.int(backend.deviceIdx),
			ptr,
			C.uint32_t(1),
		) != 0 {
			err := NewCUDAKernelError(
				kernel.KernelErrDispatchFailed,
				errors.New("unified bitwise dispatch failed"),
				"Execute",
				1,
			)

			kv := kernel.CorrelationKeyvalsFlat(ptr)
			merged := make([]any, 0, len(kv)+4)
			merged = append(merged, kv...)
			merged = append(merged, "device_idx", backend.deviceIdx, "slot", slot)

			backend.observer.Error("cuda.Backend.Execute", err, merged...)

			return err
		}
	}
}
