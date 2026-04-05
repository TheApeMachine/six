package kadabra

import (
	"math/bits"
	"sync"
)

/*
defaultKadabraRoutingBits is the XOR routing tree depth when config does not
supply kadabra.bits (e.g. core.Cfg unset). It must match the NodeID width
semantic used elsewhere in the DHT.
*/
const defaultKadabraRoutingBits = 64

type kadabraBucket struct {
	mu              sync.RWMutex
	Index           int
	Entries         []*kadabraPeer
	Candidates      map[NodeID]*kadabraPeer
	PreviousEntries []*kadabraPeer
	PreviousScore   float64
	ExploreNext     bool
	QueryCount      int
	Samples         map[NodeID]*kadabraPeerSample
}

/*
computeKadabraBucketIndex returns the bucket index for remote relative to local
using a fixed routing bit width (len(node.buckets) on each node).
*/
func computeKadabraBucketIndex(local NodeID, remote NodeID, routingBits int) int {
	if routingBits <= 0 {
		routingBits = defaultKadabraRoutingBits
	}

	distance := xorDistance(local, remote)

	if distance == 0 {
		return routingBits - 1
	}

	index := bits.LeadingZeros64(distance)

	if index >= routingBits {
		return routingBits - 1
	}

	return index
}

func (node *KadabraNode) bucketSecurityThreshold(bucketIndex int) float64 {
	if bucketIndex >= 0 && bucketIndex < len(node.BucketSecurityThresholds) {
		return node.BucketSecurityThresholds[bucketIndex]
	}

	return node.SecurityThreshold
}
