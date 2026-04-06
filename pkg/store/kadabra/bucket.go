package kadabra

import (
	"math/bits"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric"
)

/*
Bucket holds the routing entries and candidate peers for one
XOR distance prefix in the Kadabra routing table.
*/
type Bucket struct {
	mu              sync.RWMutex
	Index           int
	Entries         PeerSet
	Candidates      map[uint64]*Peer
	PreviousEntries PeerSet
	PreviousScore   float64
	ExploreNext     bool
	QueryCount      int
	Samples         map[uint64]*PeerSample
}

/*
IndexFor returns the bucket index for remote relative to local
using a fixed routing bit width. When local and remote coincide
(distance zero) it returns -1 so callers can reject self-routing.
*/
func IndexFor(local uint64, remote uint64, routingBits int) int {
	if routingBits <= 0 {
		routingBits = core.Cfg.Kadabra.Bits
	}

	distance := numeric.XOR(local, remote)

	if distance == 0 {
		return -1
	}

	return int(bits.LeadingZeros64(distance))
}
