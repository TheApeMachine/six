//go:build cuda && cgo

package cuda

/*
#cgo LDFLAGS: -L${SRCDIR} -lresolver -lcudart
#include <stdint.h>
int resolve_resonance_cuda(
    const void* graph_nodes_ptr,
    uint32_t num_nodes,
    const void* active_context_ptr,
    uint64_t* out_result
);
*/
import "C"
import (
	"unsafe"
)

//go:generate nvcc -lib resolver.cu -o libresolver.a

type CUDABackend struct{}

func (backend *CUDABackend) Available() bool {
	return true
}

func (backend *CUDABackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 || graphNodes == nil || context == nil {
		return 0, nil
	}

	var packed C.uint64_t

	status := C.resolve_resonance_cuda(
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

type CUDAError string

const (
	CUDAErrorUnavailable   CUDAError = "cuda backend unavailable"
	CUDAErrorResolveFailed CUDAError = "cuda backend resolve failed"
)

func (err CUDAError) Error() string {
	return string(err)
}
