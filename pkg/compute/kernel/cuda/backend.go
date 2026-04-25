//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lbackend -lcudart
#include <stdint.h>
int cuda_device_count();
void cleanup_cuda_pools();

int nearest_affinity_cuda(int device_id, void* query_host, void* candidates_host, uint32_t count, uint64_t* best_packed_result_host);

int hypercube_gossip_cuda(
    int       device_id,
    uint64_t* value_frames_host,
    uint32_t  value_count,
    uint32_t  d_max,
    uint32_t  fold_op
);

int geometric_cuda(
    int device_id,
    void* a_host,
    uint32_t num_values
);
*/
import "C"
import (
	"context"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

//go:generate nvcc -lib backend.cu -o libbackend.a -std=c++11

/*
Backend dispatches Value-native GPU kernels on NVIDIA CUDA devices.
The in-band VM (UniversalBitwise) runs in-process; geometric_cuda and
other kernels stay on device.
*/
type Backend struct {
	initOnce    sync.Once
	deviceCount int
	deviceIdx   int
	ctx         context.Context
	cancel      context.CancelFunc
}

type backendOption func(*Backend)

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

func (backend *Backend) Close() error {
	if backend.cancel != nil {
		backend.cancel()
	}

	C.cleanup_cuda_pools()
	return nil
}

func (backend *Backend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())

		if backend.deviceCount < 0 {
			backend.deviceCount = 0
		}
	})
}

func Available() int {
	b := NewBackend(0)
	b.init()

	return b.deviceCount
}

const cudaCommunityBreakEven = 16

func (backend *Backend) HypercubeGossip(value *primitive.Value, community []*primitive.Value) []*primitive.Value {
	n := len(community)
	if n == 0 {
		return nil
	}

	frames := make([]primitive.Value, n)
	for i, v := range community {
		frames[i] = *v
	}

	dMax := uint32(0)
	if n > 1 {
		// Calculate log2 of (n - 1)
		for v := uint32(n - 1); v > 0; v >>= 1 {
			dMax++
		}
	}

	C.hypercube_gossip_cuda(
		C.int(backend.deviceIdx),
		(*C.uint64_t)(unsafe.Pointer(&frames[0])),
		C.uint32_t(n),
		C.uint32_t(dMax),
		1, // fold_op = XOR
	)

	for i, v := range community {
		*v = frames[i]
	}

	return nil
}

func (backend *Backend) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	res := C.geometric_cuda(C.int(backend.deviceIdx), value, 1)
	return res == 0
}
