package cluster

import (
	"context"

	"github.com/theapemachine/six/pkg/primitive"
)

// ControlPlane owns the Kademlia routing table and is the single entry point
// for inserting Values into the cluster and querying for nearest neighbors.
// Each k-bucket in the routing table has its own LSM, giving O(log N) routed
// spatial queries across the full Value population.
type ControlPlane struct {
	rt *RoutingTable
}

// NewControlPlane creates a ControlPlane. The local NodeID is derived from
// the affinity of the first inserted Value; until then it uses 0 as a
// placeholder (the routing table self-corrects on first Insert).
func NewControlPlane() *ControlPlane {
	return &ControlPlane{
		rt: NewRoutingTable(0),
	}
}

// Insert adds a Value to the routing table and its bucket's LSM.
func (cp *ControlPlane) Insert(value primitive.Value) {
	// Bootstrap local ID from the first Value inserted.
	if !cp.rt.isBootstrapped() {
		cp.rt.SetLocal(NodeID(value[affinityWordIndex()]))
	}
	cp.rt.Store(&value)
}

// FindClosest returns the K Values whose affinity is closest (by XOR distance)
// to the target affinity.
func (cp *ControlPlane) FindClosest(targetAffinity uint64) []primitive.Value {
	entries := cp.rt.FindNode(context.Background(), NodeID(targetAffinity))
	values := make([]primitive.Value, len(entries))
	for i, e := range entries {
		values[i] = *e.value
	}
	return values
}

// QueryHamming returns all Value frames within maxDistance Hamming distance
// of targetAffinity, routed via the Kademlia tree.
func (cp *ControlPlane) QueryHamming(targetAffinity uint64, maxDistance int) []primitive.Value {
	frames := cp.rt.QueryHamming(targetAffinity, maxDistance)
	values := make([]primitive.Value, len(frames))
	for i, f := range frames {
		values[i] = primitive.Value(f)
	}
	return values
}
