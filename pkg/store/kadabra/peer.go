package kadabra

import "sort"

/*
PeerInfo describes a peer known by a Kadabra node.
*/
type PeerInfo struct {
	ID     NodeID
	RTT    float64
	Bucket int
}

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

	bucketIndex := kadabraBucketIndex(node.ID, peer.ID)
	bucket := node.buckets[bucketIndex]

	if existing := bucket.Candidates[peer.ID]; existing != nil {
		existing.Node = peer
		existing.RTT = rtt
		for _, entry := range bucket.Entries {
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

	peers := make([]*kadabraPeer, 0, node.BucketSize*dhtIDBits)
	for _, bucket := range node.buckets {
		peers = append(peers, bucket.Entries...)
	}

	sort.Slice(peers, func(leftIndex int, rightIndex int) bool {
		left := peers[leftIndex]
		right := peers[rightIndex]
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

		seen[peer.ID] = struct{}{}
		out = append(out, PeerInfo{
			ID:     peer.ID,
			RTT:    peer.RTT,
			Bucket: kadabraBucketIndex(node.ID, peer.ID),
		})
		if len(out) >= limit {
			break
		}
	}

	return out
}
