package cpu

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/internal/resolve"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

/*
CPUBackend resolves nearest-node queries using a zero-allocation integer distance scan.
*/
type CPUBackend struct{}

/*
Available always returns true for the CPU backend.
*/
func (backend *CPUBackend) Available() bool {
	return true
}

/*
Resolve finds the graph node with the smallest GF(257) geometric distance
to the context rotation using direct integer arithmetic.
*/
func (backend *CPUBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	context unsafe.Pointer,
) (uint64, error) {
	if numNodes <= 0 {
		return 0, fmt.Errorf("invalid numNodes: must be > 0")
	}
	if graphNodes == nil {
		return 0, fmt.Errorf("nil graphNodes pointer")
	}
	if context == nil {
		return 0, fmt.Errorf("nil context pointer")
	}

	nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
	ctx := (*geometry.GFRotation)(context)
	return resolve.PackedNearest(nodes, *ctx), nil
}


