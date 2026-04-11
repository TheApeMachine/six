package kadabra

import (
	"cmp"
	"slices"

	"github.com/theapemachine/six/pkg/core/numeric"
)

/*
UnknownPeerBucket marks LookupNodes entries for the local node,
which has no XOR routing bucket.
*/
const UnknownPeerBucket = -1

/*
UnknownPeerRTT is returned in PeerInfo.RTT when peerRTT has no
candidate (missing routing state), distinct from a measured 0 RTT.
*/
const UnknownPeerRTT = -1

/*
Peer represents a routing candidate inside a Kadabra bucket.
*/
type Peer struct {
	ID           uint64
	Affinity     []uint64
	Node         *Node
	RTT          float64
	Bucket       int
	Queries      int
	LatencyTotal float64
}

func NewPeer(
	id uint64,
	aff []uint64,
	node *Node,
	rtt float64,
	bucket int,
	queries int,
	latencyTotal float64,
) *Peer {
	return &Peer{
		ID:       id,
		Affinity: aff,
		Node:     node,
		RTT:      rtt,
		Bucket:   bucket,
	}
}

/*
PeerSet is a slice of peers with methods for the common operations
that kadabra's lookup and routing code needs.
*/
type PeerSet []*Peer

/*
Merge combines two peer sets, keeping the peer with the lowest RTT
when duplicates exist, and returns a new set sorted by XOR distance
to the target.
*/
func (peers PeerSet) Merge(other PeerSet, target uint64) PeerSet {
	merged := make(map[uint64]*Peer, len(peers)+len(other))

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		merged[peer.ID] = peer
	}

	for _, peer := range other {
		if peer == nil {
			continue
		}

		if existing := merged[peer.ID]; existing != nil {
			if peer.RTT < existing.RTT {
				merged[peer.ID] = peer
			}

			continue
		}

		merged[peer.ID] = peer
	}

	out := make(PeerSet, 0, len(merged))

	for _, peer := range merged {
		if peer == nil {
			continue
		}

		out = append(out, peer)
	}

	out.SortByDistance(target)

	return out
}

/*
Dedup removes duplicate peers by ID, preserving order.
*/
func (peers PeerSet) Dedup() PeerSet {
	seen := make(map[uint64]struct{}, len(peers))
	out := make(PeerSet, 0, len(peers))

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		if _, exists := seen[peer.ID]; exists {
			continue
		}

		seen[peer.ID] = struct{}{}
		out = append(out, peer)
	}

	return out
}

/*
SortByID sorts peers by their ID in ascending order.
*/
func (peers PeerSet) SortByID() {
	slices.SortFunc(peers, func(left *Peer, right *Peer) int {
		if left == nil {
			return 1
		}

		if right == nil {
			return -1
		}

		return cmp.Compare(left.ID, right.ID)
	})
}

/*
SortByDistance sorts peers by XOR distance to the target,
breaking ties by ID.
*/
func (peers PeerSet) SortByDistance(target uint64) {
	slices.SortFunc(peers, func(left *Peer, right *Peer) int {
		if left == nil {
			return 1
		}

		if right == nil {
			return -1
		}

		leftDist := numeric.XOR(left.ID, target)
		rightDist := numeric.XOR(right.ID, target)

		if leftDist == rightDist {
			return cmp.Compare(left.ID, right.ID)
		}

		return cmp.Compare(leftDist, rightDist)
	})
}

/*
AverageScores returns the mean of a score map, or (0, false) when empty.
*/
func (peers PeerSet) AverageScores(scores map[uint64]float64) (float64, bool) {
	if len(scores) == 0 || len(peers) == 0 {
		return 0, false
	}

	total := 0.0
	count := 0

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		if score, ok := scores[peer.ID]; ok {
			total += score
			count++
		}
	}

	if count == 0 {
		return 0, false
	}

	return total / float64(count), true
}

/*
WorstScore returns the peer ID with the lowest score.
*/
func (peers PeerSet) WorstScore(scores map[uint64]float64) uint64 {
	var worstPeer uint64
	first := true
	worstVal := 0.0

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		score, ok := scores[peer.ID]

		if !ok {
			continue
		}

		if first || score < worstVal || (score == worstVal && peer.ID < worstPeer) {
			first = false
			worstPeer = peer.ID
			worstVal = score
		}
	}

	return worstPeer
}

/*
NextBatch returns up to limit peers not yet in the seen set.
*/
func (peers PeerSet) NextBatch(seen map[uint64]struct{}, limit int) PeerSet {
	if limit <= 0 {
		return nil
	}

	batch := make(PeerSet, 0, limit)

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		if _, exists := seen[peer.ID]; exists {
			continue
		}

		batch = append(batch, peer)

		if len(batch) >= limit {
			break
		}
	}

	return batch
}

/*
Connect links two nodes as mutual peers with the given RTT.
*/
func Connect(left *Node, right *Node, rtt float64) {
	if left == nil || right == nil {
		return
	}

	left.routing.AddPeer(right, rtt)
	right.routing.AddPeer(left, rtt)
}
