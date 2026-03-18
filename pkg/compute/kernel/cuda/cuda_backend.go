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
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/numeric/geometry"
	"github.com/theapemachine/six/pkg/system/pool"
)

//go:generate nvcc -lib resolver.cu -o libresolver.a -std=c++11

type CUDABackend struct {
	initOnce    sync.Once
	deviceCount int
}

func (backend *CUDABackend) init() {
	backend.initOnce.Do(func() {
		backend.deviceCount = int(C.cuda_device_count())
		if backend.deviceCount < 0 {
			backend.deviceCount = 0
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
	if numNodes <= 0 {
		return 0, CUDAErrorResolveFailed
	}
	if graphNodes == nil {
		return 0, CUDAErrorResolveFailed
	}
	if context == nil {
		return 0, CUDAErrorResolveFailed
	}

	if !backend.Available() {
		return 0, CUDAErrorUnavailable
	}

	if numNodes < 1024 {
		nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
		ctx := (*geometry.GFRotation)(context)
		return resolve.PackedNearest(nodes, *ctx), nil
	}
	if uint64(numNodes) > uint64(^uint32(0)) {
		return 0, CUDAErrorResolveFailed
	}

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

	workerPool := pool.New(context.Background(), 1, backend.deviceCount, &pool.Config{})
	defer workerPool.Close()

	resultChans := make([]chan *pool.Result, 0, backend.deviceCount)

	chunkSize := (numNodes + backend.deviceCount - 1) / backend.deviceCount
	stride := unsafe.Sizeof(geometry.GFRotation{})

	for dev := 0; dev < backend.deviceCount; dev++ {
		start := dev * chunkSize
		if start >= numNodes {
			break
		}
		end := start + chunkSize
		if end > numNodes {
			end = numNodes
		}

		deviceID := dev
		offset := start
		length := end - start
		resultChans = append(resultChans, workerPool.Schedule(
			fmt.Sprintf("cuda-resolve-%d-%d", deviceID, offset),
			func(ctx context.Context) (any, error) {
				shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(offset)*stride)

				var packed C.uint64_t
				status := C.resolve_resonance_cuda(
					C.int(deviceID),
					shardPtr,
					C.uint32_t(length),
					context,
					&packed,
				)
				if status != 0 {
					return uint64(0), CUDAErrorResolveFailed
				}

				return numeric.RebasePackedID(uint64(packed), offset), nil
			},
		))
	}

	var best uint64

	for _, resultCh := range resultChans {
		result := <-resultCh
		if result.Error != nil {
			return 0, result.Error
		}

		rebased, ok := result.Value.(uint64)
		if !ok {
			return 0, CUDAErrorResolveFailed
		}

		if rebased > best {
			best = rebased
		}
	}

	return best, nil
}

type CUDAError string

const (
	CUDAErrorUnavailable   CUDAError = "cuda backend unavailable"
	CUDAErrorResolveFailed CUDAError = "cuda backend resolve failed"
)

func (err CUDAError) Error() string {
	return string(err)
}
