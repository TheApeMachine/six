package kadabra

import (
	"sort"

	"github.com/theapemachine/six/pkg/core"
)

/*
LookupTrace records the nodes queried during a lookup.
*/
type LookupTrace struct {
	Key     uint64
	Nodes   []NodeID
	Latency float64
	Found   bool
}

/*
FindRecord performs an iterative Kadabra lookup for the given key.
*/
func (node *KadabraNode) FindRecord(key uint64) (SequenceRecord, bool, LookupTrace) {
	trace := LookupTrace{
		Key:   key,
		Nodes: []NodeID{node.ID},
	}

	node.recordsMu.RLock()
	if record, exists := node.records[key]; exists {
		node.recordsMu.RUnlock()
		trace.Found = true
		return record, true, trace
	}
	node.recordsMu.RUnlock()

	target := NodeID(key)
	seen := map[NodeID]struct{}{node.ID: {}}
	shortlist := node.closestLookupPeers(target)

	for {
		batch := nextLookupBatch(shortlist, seen, node.LookupParallelism)
		if len(batch) == 0 {
			break
		}

		progress := false
		for _, peer := range batch {
			progress = true
			seen[peer.ID] = struct{}{}
			trace.Nodes = append(trace.Nodes, peer.ID)
			trace.Latency += peer.RTT
			node.observePeerQuery(peer)

			peer.Node.recordsMu.RLock()
			record, exists := peer.Node.records[key]
			peer.Node.recordsMu.RUnlock()
			if exists {
				trace.Found = true
				return record, true, trace
			}

			shortlist = mergeLookupPeers(shortlist, peer.Node.closestLookupPeers(target), target)
		}

		if !progress {
			break
		}
	}

	return SequenceRecord{}, false, trace
}

/*
LookupNodes returns up to limit closest node ids discovered by iterative lookup.
*/
func (node *KadabraNode) LookupNodes(target uint64, limit int) []PeerInfo {
	nodes := node.lookupNodes(NodeID(target), limit)
	targetID := NodeID(target)
	out := make([]PeerInfo, 0, len(nodes)+1)

	for _, candidate := range nodes {
		if candidate == nil || candidate.ID == node.ID {
			continue
		}

		rtt := node.peerRTT(candidate.ID)
		out = append(out, PeerInfo{
			ID:     candidate.ID,
			RTT:    rtt,
			Bucket: kadabraBucketIndex(node.ID, candidate.ID),
		})
	}

	out = append(out, PeerInfo{
		ID:     node.ID,
		RTT:    0,
		Bucket: core.Cfg.Kadabra.Bits - 1,
	})

	sort.Slice(out, func(leftIndex int, rightIndex int) bool {
		leftDistance := xorDistance(out[leftIndex].ID, targetID)
		rightDistance := xorDistance(out[rightIndex].ID, targetID)
		if leftDistance == rightDistance {
			return out[leftIndex].ID < out[rightIndex].ID
		}

		return leftDistance < rightDistance
	})

	return out
}

func (node *KadabraNode) lookupNodes(target NodeID, limit int) []*KadabraNode {
	if limit <= 0 {
		return nil
	}

	shortlist := node.closestLookupPeers(target)
	seen := map[NodeID]struct{}{node.ID: {}}
	discovered := map[NodeID]*KadabraNode{
		node.ID: node,
	}

	for {
		batch := nextLookupBatch(shortlist, seen, node.LookupParallelism)
		if len(batch) == 0 {
			break
		}

		progress := false
		for _, peer := range batch {
			progress = true
			seen[peer.ID] = struct{}{}
			discovered[peer.ID] = peer.Node
			node.observePeerQuery(peer)
			shortlist = mergeLookupPeers(shortlist, peer.Node.closestLookupPeers(target), target)
		}

		if !progress {
			break
		}
	}

	nodes := make([]*KadabraNode, 0, len(discovered))
	for _, candidate := range discovered {
		nodes = append(nodes, candidate)
	}

	sort.Slice(nodes, func(leftIndex int, rightIndex int) bool {
		left := nodes[leftIndex]
		right := nodes[rightIndex]
		leftDistance := xorDistance(left.ID, target)
		rightDistance := xorDistance(right.ID, target)
		if leftDistance == rightDistance {
			return left.ID < right.ID
		}

		return leftDistance < rightDistance
	})

	if len(nodes) > limit {
		nodes = nodes[:limit]
	}

	return nodes
}

func nextLookupBatch(peers []*kadabraPeer, seen map[NodeID]struct{}, limit int) []*kadabraPeer {
	if limit <= 0 {
		return nil
	}

	batch := make([]*kadabraPeer, 0, limit)

	for _, peer := range peers {
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

func xorDistance(left NodeID, right NodeID) uint64 {
	return uint64(left ^ right)
}
