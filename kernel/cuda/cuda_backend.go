//go:build cuda

package cuda

import (
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel/internal/resolve"
)

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

	nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
	ctx := (*geometry.GFRotation)(context)

	return resolve.PackedNearest(nodes, *ctx), nil
}
