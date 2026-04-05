package kadabra

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/theapemachine/six/pkg/store/frankentrie"
)

/*
NodeID is the 64-bit identifier used by the Kadabra routing layer.
*/
type NodeID uint64

/*
NodeOption configures a KadabraNode.
*/
type NodeOption func(*KadabraNode)

/*
KadabraNode wraps a SequenceStore in a Kademlia-style DHT node with per-bucket
adaptive peer selection inspired by Kadabra.
*/
type KadabraNode struct {
	ID                       NodeID
	Store                    *frankentrie.Store
	BucketSize               int
	ReplicationFactor        int
	LookupParallelism        int
	EpochQueries             int
	Penalty                  float64
	SecurityThreshold        float64
	BucketSecurityThresholds []float64
	random                   *rand.Rand
	recordsMu                sync.RWMutex
	records                  map[uint64]SequenceRecord
	buckets                  [dhtIDBits]*kadabraBucket
}

/*
NewKadabraNode constructs a Kadabra DHT node backed by a SequenceStore.
*/
func NewKadabraNode(id NodeID, options ...NodeOption) *KadabraNode {
	node := &KadabraNode{
		ID:                id,
		BucketSize:        defaultKadabraBucketSize,
		ReplicationFactor: defaultKadabraReplication,
		LookupParallelism: defaultKadabraAlpha,
		EpochQueries:      defaultKadabraEpochQueries,
		records:           make(map[uint64]SequenceRecord),
	}

	for bucketIndex := range node.buckets {
		node.buckets[bucketIndex] = &kadabraBucket{
			Index:         bucketIndex,
			Candidates:    make(map[NodeID]*kadabraPeer),
			PreviousScore: math.Inf(-1),
			Samples:       make(map[NodeID]*kadabraPeerSample),
		}
	}

	for _, option := range options {
		option(node)
	}

	if node.Store == nil {
		node.Store = frankentrie.NewStore()
	}

	if node.random == nil {
		node.random = rand.New(rand.NewSource(int64(id) + 1))
	}

	return node
}

/*
Publish stores a labeled sequence on the replicationFactor closest DHT nodes and
trains the local frankentrie.Store on each replica.
*/
func (node *KadabraNode) Publish(sequence string, label string) (SequenceRecord, error) {
	record := SequenceRecord{
		Key:       HashSequenceRecord(sequence, label),
		Sequence:  sequence,
		Label:     label,
		Publisher: node.ID,
	}

	replicas := node.lookupNodes(NodeID(record.Key), node.ReplicationFactor)
	if len(replicas) == 0 {
		replicas = []*KadabraNode{node}
	}

	for _, replica := range replicas {
		if err := replica.StoreRecord(record); err != nil {
			return record, err
		}
	}

	return record, nil
}

/*
StoreRecord stores a replicated sequence record locally and trains the backing
frankentrie.Store on each replica once per key.
*/
func (node *KadabraNode) StoreRecord(record SequenceRecord) error {
	node.recordsMu.Lock()
	defer node.recordsMu.Unlock()

	if existing, exists := node.records[record.Key]; exists {
		if existing.Sequence == record.Sequence && existing.Label == record.Label {
			return nil
		}

		return fmt.Errorf(
			"kadabra: record %d conflict: stored sequence %q label %q vs incoming sequence %q label %q",
			record.Key,
			existing.Sequence,
			existing.Label,
			record.Sequence,
			record.Label,
		)
	}

	node.records[record.Key] = record
	node.Store.Insert(record.Sequence, record.Label)

	return nil
}

/*
HasRecord reports whether the node stores the given DHT key locally.
*/
func (node *KadabraNode) HasRecord(key uint64) bool {
	node.recordsMu.RLock()
	defer node.recordsMu.RUnlock()

	_, exists := node.records[key]

	return exists
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
		Bucket: dhtIDBits - 1,
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

/*
WithLocalStore installs the local frankentrie.Store backing the node.
*/
func WithLocalStore(store *frankentrie.Store) NodeOption {
	return func(node *KadabraNode) {
		node.Store = store
	}
}

/*
WithBucketSize overrides the maximum peers held in each bucket.
*/
func WithBucketSize(bucketSize int) NodeOption {
	return func(node *KadabraNode) {
		if bucketSize <= 0 {
			return
		}

		node.BucketSize = bucketSize
	}
}

/*
WithReplicationFactor overrides how many closest nodes store each record.
*/
func WithReplicationFactor(replicationFactor int) NodeOption {
	return func(node *KadabraNode) {
		if replicationFactor <= 0 {
			return
		}

		node.ReplicationFactor = replicationFactor
	}
}

/*
WithLookupParallelism overrides how many peers are queried in each lookup step.
*/
func WithLookupParallelism(alpha int) NodeOption {
	return func(node *KadabraNode) {
		if alpha <= 0 {
			return
		}

		node.LookupParallelism = alpha
	}
}

/*
WithEpochQueries overrides how many peer queries form one Kadabra epoch.
*/
func WithEpochQueries(epochQueries int) NodeOption {
	return func(node *KadabraNode) {
		if epochQueries <= 0 {
			return
		}

		node.EpochQueries = epochQueries
	}
}

/*
WithPenalty overrides the scoring penalty for peers not used during an epoch.
When zero or negative, the node derives the penalty from the epoch mean.
*/
func WithPenalty(penalty float64) NodeOption {
	return func(node *KadabraNode) {
		node.Penalty = penalty
	}
}

/*
WithSecurityThreshold rejects exploration candidates below the given RTT floor.
*/
func WithSecurityThreshold(threshold float64) NodeOption {
	return func(node *KadabraNode) {
		if threshold < 0 {
			return
		}

		node.SecurityThreshold = threshold
	}
}

/*
WithBucketSecurityThresholds sets per-bucket exploration RTT floors.
*/
func WithBucketSecurityThresholds(thresholds []float64) NodeOption {
	return func(node *KadabraNode) {
		node.BucketSecurityThresholds = append([]float64(nil), thresholds...)
	}
}

/*
WithNodeRandomSource installs the random source used by Kadabra exploration.
*/
func WithNodeRandomSource(source rand.Source) NodeOption {
	return func(node *KadabraNode) {
		if source == nil {
			return
		}

		node.random = rand.New(source)
	}
}
