package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel/internal/resolve"
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
	if numNodes <= 0 || graphNodes == nil || context == nil {
		return 0, nil
	}

	nodes := unsafe.Slice((*geometry.GFRotation)(graphNodes), numNodes)
	ctx := (*geometry.GFRotation)(context)
	return resolve.PackedNearest(nodes, *ctx), nil
}
