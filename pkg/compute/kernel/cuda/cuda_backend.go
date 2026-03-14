//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lresolver -lcudart
#include <stdint.h>
int cuda_device_count();
int resolve_resonance_cuda(
    int device_id,
    const void* graph_nodes_ptr,
    uint32_t num_nodes,
    const void* active_context_ptr,
    uint64_t* out_result
);
*/
import "C"
import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/numeric"
)

//go:generate nvcc -lib resolver.cu -o libresolver.a -std=c++11

type CUDABackend struct {
	initOnce     sync.Once
	deviceCount  int
}

func (backend *CUDABackend) init() {
	backend.initOnce.Do(func() {
		if backend.deviceCount == 0 {
			count := int(C.cuda_device_count())
			if count < 1 {
				count = 1 // Default to 1 to attempt, though Available will probably be false if truly 0
			}
			backend.deviceCount = count
		}
	})
}

func (backend *CUDABackend) Available() bool {
	backend.init()
	return backend.deviceCount > 0
}

func (backend *CUDABackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 || graphNodes == nil || context == nil {
		return 0, nil
	}

	if !backend.Available() {
		return 0, CUDAErrorUnavailable
	}

	backend.init()

	if backend.deviceCount == 1 {
		var packed C.uint64_t
		status := C.resolve_resonance_cuda(
			0,
			graphNodes,
			C.uint32_t(numNodes),
			context,
			&packed,
		)

		if status != 0 {
			return 0, CUDAErrorResolveFailed
		}
		return uint64(packed), nil
	}

	// Multi-GPU Orchestration
	var best atomic.Uint64
	var wg sync.WaitGroup
	var errOnce sync.Once
	var aggregateErr error

	chunkSize := (numNodes + backend.deviceCount - 1) / backend.deviceCount

	for dev := 0; dev < backend.deviceCount; dev++ {
		start := dev * chunkSize
		if start >= numNodes {
			break
		}
		end := start + chunkSize
		if end > numNodes {
			end = numNodes
		}

		wg.Add(1)
		go func(deviceID, offset, length int) {
			defer wg.Done()

			shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(offset*4)) // GFRotation is 4 bytes

			var packed C.uint64_t
			status := C.resolve_resonance_cuda(
				C.int(deviceID),
				shardPtr,
				C.uint32_t(length),
				context,
				&packed,
			)

			if status != 0 {
				errOnce.Do(func() {
					aggregateErr = CUDAErrorResolveFailed
				})
				return
			}

			rebased := numeric.RebasePackedID(uint64(packed), offset)

			// atomicMax implementation
			for {
				current := best.Load()
				if rebased <= current {
					break
				}
				if best.CompareAndSwap(current, rebased) {
					break
				}
			}
		}(dev, start, end-start)
	}

	wg.Wait()

	if aggregateErr != nil {
		return 0, aggregateErr
	}

	return best.Load(), nil
}

type CUDAError string

const (
	CUDAErrorUnavailable   CUDAError = "cuda backend unavailable"
	CUDAErrorResolveFailed CUDAError = "cuda backend resolve failed"
)

func (err CUDAError) Error() string {
	return string(err)
}
