package cluster

import (
	"context"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
ControlPlane owns the Kademlia routing table and is the single entry point
for inserting Values into the cluster and querying for nearest neighbors.
Each k-bucket in the routing table has its own LSM (TokenID → Value bitmap).
*/
type ControlPlane struct {
	ctx    context.Context
	cancel context.CancelFunc
	rt     *RoutingTable
}

/*
NewControlPlane creates a ControlPlane. The local NodeID is derived from
the affinity of the first inserted Value; until then it uses 0 as a
placeholder (the routing table self-corrects on first Insert).
*/
func NewControlPlane(ctx context.Context) *ControlPlane {
	return &ControlPlane{
		rt: NewRoutingTable(0),
	}
}

/*
Insert adds a Value to the routing table and its bucket's LSM.
*/
func (cp *ControlPlane) Insert(value primitive.Value) {
	// Bootstrap local ID from the first Value inserted.
	if !cp.rt.isBootstrapped() {
		cp.rt.SetLocal(NodeID(value[affinityWordIndex()]))
	}

	cp.rt.Store(&value)
}

/*
FindClosest returns the K Values whose affinity is closest (by XOR distance)
to the target affinity.
*/
func (cp *ControlPlane) FindClosest(targetAffinity uint64) []primitive.Value {
	entries := cp.rt.FindNode(context.Background(), NodeID(targetAffinity))
	values := make([]primitive.Value, len(entries))

	for i, e := range entries {
		values[i] = *e.value
	}

	return values
}
