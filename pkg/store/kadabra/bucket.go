package kadabra

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
)

type kadabraBucket struct {
	Index           int
	Entries         []*kadabraPeer
	Candidates      map[NodeID]*kadabraPeer
	PreviousEntries []*kadabraPeer
	PreviousScore   float64
	ExploreNext     bool
	QueryCount      int
	Samples         map[NodeID]*kadabraPeerSample
}

func kadabraBucketIndex(local NodeID, remote NodeID) int {
	distance := xorDistance(local, remote)

	if distance == 0 {
		return core.Cfg.Kadabra.Bits - 1
	}

	index := bits.LeadingZeros64(distance)

	if index >= core.Cfg.Kadabra.Bits {
		return core.Cfg.Kadabra.Bits - 1
	}

	return index
}

func (node *KadabraNode) bucketSecurityThreshold(bucketIndex int) float64 {
	if bucketIndex >= 0 && bucketIndex < len(node.BucketSecurityThresholds) {
		return node.BucketSecurityThresholds[bucketIndex]
	}

	return node.SecurityThreshold
}
