package kadabra

import (
	"sort"
)

/*
PeerInfo describes a peer known by a Kadabra node.
*/
type PeerInfo struct {
	ID     NodeID
	RTT    float64
	Bucket int
}

// UnknownPeerBucket marks LookupNodes entries for the local node, which has no XOR routing bucket.
const UnknownPeerBucket = -1

// UnknownPeerRTT is returned in PeerInfo.RTT when peerRTT has no candidate (missing routing state), distinct from a measured 0 RTT.
const UnknownPeerRTT = -1

type kadabraPeer struct {
	ID   NodeID
	Node *KadabraNode
	RTT  float64
}

type kadabraPeerSample struct {
	Queries      int
	LatencyTotal float64
}

/*
Connect links two nodes as mutual peers with the given RTT.
*/
func Connect(left *KadabraNode, right *KadabraNode, rtt float64) {
	if left == nil || right == nil {
		return
	}

	left.AddPeer(right, rtt)
	right.AddPeer(left, rtt)
}

/*
AddPeer registers a peer as a routing candidate and seeds the bucket when there
is still free capacity.
*/
func (node *KadabraNode) AddPeer(peer *KadabraNode, rtt float64) {
	if peer == nil || peer.ID == node.ID {
		return
	}

	bucketIndex := node.bucketIndexForPeer(peer.ID)
	if bucketIndex < 0 || bucketIndex >= len(node.buckets) {
		return
	}

	bucket := node.buckets[bucketIndex]
	if bucket == nil {
		return
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	if existing := bucket.Candidates[peer.ID]; existing != nil {
		existing.Node = peer
		existing.RTT = rtt
		for _, entry := range bucket.Entries {
			if entry == nil {
				continue
			}

			if entry.ID == peer.ID {
				entry.Node = peer
				entry.RTT = rtt
			}
		}

		return
	}

	candidate := &kadabraPeer{
		ID:   peer.ID,
		Node: peer,
		RTT:  rtt,
	}

	bucket.Candidates[peer.ID] = candidate

	if len(bucket.Entries) < node.BucketSize {
		bucket.Entries = append(bucket.Entries, clonePeer(candidate))
		sortPeersByID(bucket.Entries)
	}
}

/*
ClosestPeers returns up to limit peers from the node's current routing table,
ordered by XOR distance to the target.
*/
func (node *KadabraNode) ClosestPeers(target NodeID, limit int) []PeerInfo {
	if limit <= 0 {
		return nil
	}

	capacity := node.peerTableCapacity()
	peers := make([]*kadabraPeer, 0, capacity)

	for _, bucket := range node.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()
		entries := bucket.Entries
		bucket.mu.RUnlock()
		peers = append(peers, entries...)
	}

	sort.Slice(peers, func(leftIndex int, rightIndex int) bool {
		left := peers[leftIndex]
		right := peers[rightIndex]

		if left == nil {
			return false
		}

		if right == nil {
			return true
		}

		leftDistance := xorDistance(left.ID, target)
		rightDistance := xorDistance(right.ID, target)
		if leftDistance == rightDistance {
			return left.ID < right.ID
		}

		return leftDistance < rightDistance
	})

	out := make([]PeerInfo, 0, min(limit, len(peers)))
	seen := make(map[NodeID]struct{}, len(peers))
	for _, peer := range peers {
		if _, exists := seen[peer.ID]; exists {
			continue
		}

		if peer == nil {
			continue
		}

		seen[peer.ID] = struct{}{}
		out = append(out, PeerInfo{
			ID:     peer.ID,
			RTT:    peer.RTT,
			Bucket: node.bucketIndexForPeer(peer.ID),
		})
		if len(out) >= limit {
			break
		}
	}

	return out
}

func mergeLookupPeers(
	left []*kadabraPeer,
	right []*kadabraPeer,
	target NodeID,
) []*kadabraPeer {
	merged := make(
		map[NodeID]*kadabraPeer,
		len(left)+len(right),
	)

	for _, peer := range left {
		if peer == nil {
			continue
		}

		merged[peer.ID] = peer
	}

	for _, peer := range right {
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

	out := make([]*kadabraPeer, 0, len(merged))

	for _, peer := range merged {
		if peer == nil {
			continue
		}

		out = append(out, peer)
	}

	sort.Slice(out, func(
		leftIndex int, rightIndex int,
	) bool {
		leftPeer := out[leftIndex]
		rightPeer := out[rightIndex]

		if leftPeer == nil {
			return false
		}

		if rightPeer == nil {
			return true
		}

		leftDistance := xorDistance(leftPeer.ID, target)
		rightDistance := xorDistance(rightPeer.ID, target)

		if leftDistance == rightDistance {
			return leftPeer.ID < rightPeer.ID
		}

		return leftDistance < rightDistance
	})

	return out
}

func dedupePeers(peers []*kadabraPeer) []*kadabraPeer {
	seen := make(map[NodeID]struct{}, len(peers))
	out := make([]*kadabraPeer, 0, len(peers))
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

func clonePeer(peer *kadabraPeer) *kadabraPeer {
	if peer == nil {
		return nil
	}

	return &kadabraPeer{
		ID:   peer.ID,
		Node: peer.Node,
		RTT:  peer.RTT,
	}
}

func clonePeers(peers []*kadabraPeer) []*kadabraPeer {
	cloned := make([]*kadabraPeer, 0, len(peers))
	for _, peer := range peers {
		cloned = append(cloned, clonePeer(peer))
	}

	return cloned
}

func sortPeersByID(peers []*kadabraPeer) {
	sort.Slice(peers, func(leftIndex int, rightIndex int) bool {
		left := peers[leftIndex]
		right := peers[rightIndex]

		if left == nil {
			return false
		}

		if right == nil {
			return true
		}

		return left.ID < right.ID
	})
}

func (node *KadabraNode) closestLookupPeers(
	target NodeID,
) []*kadabraPeer {
	peers := make([]*kadabraPeer, 0, node.peerTableCapacity())

	for _, bucket := range node.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()
		entries := bucket.Entries
		bucket.mu.RUnlock()
		peers = append(peers, entries...)
	}

	sort.Slice(peers, func(leftIndex int, rightIndex int) bool {
		left := peers[leftIndex]
		right := peers[rightIndex]

		if left == nil {
			return false
		}

		if right == nil {
			return true
		}

		leftDistance := xorDistance(left.ID, target)
		rightDistance := xorDistance(right.ID, target)

		if leftDistance == rightDistance {
			return left.ID < right.ID
		}

		return leftDistance < rightDistance
	})

	return dedupePeers(peers)
}

func (node *KadabraNode) observePeerQuery(peer *kadabraPeer) {
	if peer == nil {
		return
	}

	bucketIndex := node.bucketIndexForPeer(peer.ID)
	if bucketIndex < 0 || bucketIndex >= len(node.buckets) {
		return
	}

	bucket := node.buckets[bucketIndex]
	if bucket == nil {
		return
	}

	bucket.mu.Lock()
	sample := bucket.Samples[peer.ID]
	if sample == nil {
		sample = &kadabraPeerSample{}
		bucket.Samples[peer.ID] = sample
	}

	sample.Queries++
	sample.LatencyTotal += peer.RTT
	bucket.QueryCount++

	if node.EpochQueries > 0 && bucket.QueryCount >= node.EpochQueries {
		node.finishEpoch(bucket)
	}

	bucket.mu.Unlock()
}

func (node *KadabraNode) selectExplorationPeer(
	bucket *kadabraBucket,
) *kadabraPeer {
	if bucket == nil {
		return nil
	}

	currentEntries := make(map[NodeID]struct{}, len(bucket.Entries))

	for _, entry := range bucket.Entries {
		if entry == nil {
			continue
		}

		currentEntries[entry.ID] = struct{}{}
	}

	eligible := make([]*kadabraPeer, 0, len(bucket.Candidates))
	securityThreshold := node.bucketSecurityThreshold(bucket.Index)

	for _, candidate := range bucket.Candidates {
		if candidate == nil {
			continue
		}

		if _, exists := currentEntries[candidate.ID]; exists {
			continue
		}

		if candidate.RTT < securityThreshold {
			continue
		}

		eligible = append(eligible, candidate)
	}

	if len(eligible) == 0 {
		/*
			Intentional availability fallback: when every unseen candidate fails
			securityThreshold, we broaden the pool to all unseen Candidates so
			routing can still merge lookups and join the mesh (liveness). This
			weakens the RTT eclipse floor for that choice — tightening means
			raising securityThreshold / bucketSecurityThreshold, or gating this
			block behind config later. Related: securityThreshold, eligible,
			bucket.Candidates, currentEntries.
		*/
		for _, candidate := range bucket.Candidates {
			if candidate == nil {
				continue
			}

			if _, exists := currentEntries[candidate.ID]; exists {
				continue
			}

			eligible = append(eligible, candidate)
		}
	}

	if len(eligible) == 0 {
		return nil
	}

	sort.Slice(eligible, func(leftIndex int, rightIndex int) bool {
		return eligible[leftIndex].ID < eligible[rightIndex].ID
	})

	return eligible[node.random.Intn(len(eligible))]
}

func (node *KadabraNode) peerRTT(peerID NodeID) (rtt float64, ok bool) {
	bucketIndex := node.bucketIndexForPeer(peerID)
	if bucketIndex < 0 || bucketIndex >= len(node.buckets) {
		return 0, false
	}

	bucket := node.buckets[bucketIndex]
	if bucket == nil {
		return 0, false
	}

	bucket.mu.RLock()
	defer bucket.mu.RUnlock()

	if candidate := bucket.Candidates[peerID]; candidate != nil {
		return candidate.RTT, true
	}

	return 0, false
}

func averagePeerScores(scores map[NodeID]float64) (mean float64, ok bool) {
	if len(scores) == 0 {
		return 0, false
	}

	total := 0.0

	for _, score := range scores {
		total += score
	}

	return total / float64(len(scores)), true
}

func worstPeerScore(scores map[NodeID]float64) NodeID {
	var worstPeer NodeID
	first := true
	worstScore := 0.0

	for peerID, score := range scores {
		if first || score < worstScore || (score == worstScore && peerID < worstPeer) {
			first = false
			worstPeer = peerID
			worstScore = score
		}
	}

	return worstPeer
}
