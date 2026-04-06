package kadabra

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
RoutingTable manages the Kademlia-style XOR routing buckets, peer
registration, iterative lookups, and epoch-based bucket adaptation.
It is composed onto Node and owns all DHT routing state.
*/
type RoutingTable struct {
	nodeID                   uint64
	bucketSize               int
	lookupParallelism        int
	epochQueries             int
	penalty                  float64
	securityThreshold        float64
	bucketSecurityThresholds []float64
	routingBits              int
	buckets                  []*Bucket
	random                   *rand.Rand
	recordsMu                sync.RWMutex
	records                  map[uint64]SequenceRecord
	node                     *Node
}

/*
NewRoutingTable constructs a routing table for the given node.
*/
func NewRoutingTable(node *Node) *RoutingTable {
	rt := &RoutingTable{
		nodeID:            node.ID,
		bucketSize:        core.Cfg.Kadabra.BucketSize,
		lookupParallelism: core.Cfg.Kadabra.Alpha,
		epochQueries:      core.Cfg.Kadabra.EpochQueries,
		routingBits:       core.Cfg.Kadabra.Bits,
		records:           make(map[uint64]SequenceRecord),
		buckets:           make([]*Bucket, core.Cfg.Kadabra.Bits),
		node:              node,
	}

	for bucketIndex := range rt.buckets {
		rt.buckets[bucketIndex] = &Bucket{
			Index:         bucketIndex,
			Candidates:    make(map[uint64]*Peer),
			PreviousScore: math.Inf(-1),
			Samples:       make(map[uint64]*PeerSample),
		}
	}

	rt.random = rand.New(rand.NewSource(time.Now().UnixNano()))

	return rt
}

/*
AddPeer registers a peer as a routing candidate and seeds the bucket
when there is still free capacity.
*/
func (rt *RoutingTable) AddPeer(peer *Node, rtt float64) {
	if peer == nil || peer.ID == rt.nodeID {
		return
	}

	bucketIndex := rt.bucketIndexFor(peer.ID)

	if bucketIndex < 0 || bucketIndex >= len(rt.buckets) {
		return
	}

	bucket := rt.buckets[bucketIndex]

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

	candidate := &Peer{
		ID:   peer.ID,
		Node: peer,
		RTT:  rtt,
	}

	bucket.Candidates[peer.ID] = candidate

	if len(bucket.Entries) < rt.bucketSize {
		bucket.Entries = append(bucket.Entries, candidate.Clone())
		PeerSet(bucket.Entries).SortByID()
	}
}

/*
ClosestPeers returns up to limit peers ordered by XOR distance to target.
*/
func (rt *RoutingTable) ClosestPeers(target uint64, limit int) []PeerInfo {
	if limit <= 0 {
		return nil
	}

	peers := rt.allPeers()
	peers.SortByDistance(target)

	out := make([]PeerInfo, 0, min(limit, len(peers)))
	seen := make(map[uint64]struct{}, len(peers))

	for _, peer := range peers {
		if peer == nil {
			continue
		}

		if _, exists := seen[peer.ID]; exists {
			continue
		}

		seen[peer.ID] = struct{}{}
		out = append(out, PeerInfo{
			ID:     peer.ID,
			RTT:    peer.RTT,
			Bucket: rt.bucketIndexFor(peer.ID),
		})

		if len(out) >= limit {
			break
		}
	}

	return out
}

/*
ClosestNodesByAffinity returns up to limit nodes from the routing table,
sorted by affinity distance to the target.
*/
func (rt *RoutingTable) Closest(
	target *primitive.Affinity, limit int,
) []*Node {
	if limit <= 0 {
		return nil
	}

	type candidate struct {
		node     *Node
		distance int
	}

	var candidates []candidate
	seen := map[uint64]struct{}{rt.nodeID: {}}

	for _, bucket := range rt.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()

		snap := make(PeerSet, 0, len(bucket.Entries))

		for _, entry := range bucket.Entries {
			if entry == nil || entry.Node == nil {
				continue
			}

			snap = append(snap, entry.Clone())
		}

		bucket.mu.RUnlock()

		for _, entry := range snap {
			if entry == nil || entry.Node == nil {
				continue
			}

			if _, exists := seen[entry.ID]; exists {
				continue
			}

			seen[entry.ID] = struct{}{}
			candidates = append(candidates, candidate{
				node:     entry.Node,
				distance: target.Distance(entry.Node.Affinity),
			})
		}
	}

	candidates = append(candidates, candidate{
		node:     rt.node,
		distance: target.Distance(rt.node.Affinity),
	})

	sort.Slice(candidates, func(leftIdx, rightIdx int) bool {
		if candidates[leftIdx].distance == candidates[rightIdx].distance {
			return candidates[leftIdx].node.ID < candidates[rightIdx].node.ID
		}

		return candidates[leftIdx].distance < candidates[rightIdx].distance
	})

	result := make([]*Node, 0, min(limit, len(candidates)))

	for idx := 0; idx < len(candidates) && len(result) < limit; idx++ {
		result = append(result, candidates[idx].node)
	}

	return result
}

/*
FindRecord performs an iterative Kadabra lookup for the given key.
*/
func (rt *RoutingTable) FindRecord(key uint64) (SequenceRecord, bool) {
	rt.recordsMu.RLock()

	if record, exists := rt.records[key]; exists {
		rt.recordsMu.RUnlock()
		return record, true
	}

	rt.recordsMu.RUnlock()

	seen := map[uint64]struct{}{rt.nodeID: {}}
	shortlist := rt.closestLookupPeers(key)

	for {
		batch := shortlist.NextBatch(seen, rt.lookupParallelism)

		if len(batch) == 0 {
			break
		}

		for _, peer := range batch {
			seen[peer.ID] = struct{}{}

			if peer.Node == nil {
				continue
			}

			rt.observePeerQuery(peer)

			peer.Node.routing.recordsMu.RLock()
			record, exists := peer.Node.routing.records[key]
			peer.Node.routing.recordsMu.RUnlock()

			if exists {
				return record, true
			}

			shortlist = shortlist.Merge(
				peer.Node.routing.closestLookupPeers(key), key,
			)
		}
	}

	return SequenceRecord{}, false
}

/*
LookupNodes returns up to limit closest node IDs discovered by iterative lookup.
*/
func (rt *RoutingTable) LookupNodes(target uint64, limit int) []PeerInfo {
	nodes := rt.lookupNodes(target, limit)
	out := make([]PeerInfo, 0, len(nodes)+1)

	for _, candidate := range nodes {
		if candidate == nil || candidate.ID == rt.nodeID {
			continue
		}

		rtt, rttOK := rt.peerRTT(candidate.ID)

		if !rttOK {
			rtt = UnknownPeerRTT
		}

		out = append(out, PeerInfo{
			ID:     candidate.ID,
			RTT:    rtt,
			Bucket: rt.bucketIndexFor(candidate.ID),
		})
	}

	out = append(out, PeerInfo{
		ID:     rt.nodeID,
		RTT:    0,
		Bucket: UnknownPeerBucket,
	})

	sort.Slice(out, func(leftIdx, rightIdx int) bool {
		leftDist := numeric.XOR(out[leftIdx].ID, target)
		rightDist := numeric.XOR(out[rightIdx].ID, target)

		if leftDist == rightDist {
			return out[leftIdx].ID < out[rightIdx].ID
		}

		return leftDist < rightDist
	})

	return out
}

/*
HasRecord reports whether this node stores the given DHT key locally.
*/
func (rt *RoutingTable) HasRecord(key uint64) bool {
	rt.recordsMu.RLock()
	defer rt.recordsMu.RUnlock()

	_, exists := rt.records[key]

	return exists
}

/*
Store stores a replicated sequence record locally and routes
it to the appropriate trie cluster on the owning node.
*/
func (rt *RoutingTable) Store(
	record SequenceRecord, affinity *primitive.Affinity,
) error {
	rt.recordsMu.Lock()

	if existing, exists := rt.records[record.Key]; exists {
		rt.recordsMu.Unlock()

		if existing.Sequence == record.Sequence && existing.Label == record.Label {
			return nil
		}

		return NewKadabraError(
			ErrKadabraRecordConflict,
			"key", record.Key,
			"storedSequence", existing.Sequence,
			"incomingSequence", record.Sequence,
		)
	}

	rt.records[record.Key] = record
	rt.recordsMu.Unlock()

	rt.node.triesMu.Lock()

	if len(rt.node.Tries) > 0 {
		rt.node.Tries[0].Insert(record.Sequence, record.Label)
	}

	rt.node.triesMu.Unlock()

	return nil
}

func (rt *RoutingTable) bucketIndexFor(remote uint64) int {
	return IndexFor(rt.nodeID, remote, rt.routingBits)
}

func (rt *RoutingTable) peerTableCapacity() int {
	if rt.bucketSize <= 0 {
		return 0
	}

	routingBits := rt.routingBits

	if routingBits <= 0 {
		routingBits = core.Cfg.Kadabra.Bits
	}

	return rt.bucketSize * routingBits
}

func (rt *RoutingTable) allPeers() PeerSet {
	peers := make(PeerSet, 0, rt.peerTableCapacity())

	for _, bucket := range rt.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()
		peers = append(peers, bucket.Entries...)
		bucket.mu.RUnlock()
	}

	return peers
}

func (rt *RoutingTable) closestLookupPeers(target uint64) PeerSet {
	peers := rt.allPeers()
	peers.SortByDistance(target)

	return peers.Dedup()
}

func (rt *RoutingTable) observePeerQuery(peer *Peer) {
	if peer == nil {
		return
	}

	bucketIndex := rt.bucketIndexFor(peer.ID)

	if bucketIndex < 0 || bucketIndex >= len(rt.buckets) {
		return
	}

	bucket := rt.buckets[bucketIndex]

	if bucket == nil {
		return
	}

	bucket.mu.Lock()

	sample := bucket.Samples[peer.ID]

	if sample == nil {
		sample = &PeerSample{}
		bucket.Samples[peer.ID] = sample
	}

	sample.Queries++
	sample.LatencyTotal += peer.RTT
	bucket.QueryCount++

	bucket.mu.Unlock()
}

func (rt *RoutingTable) selectExplorationPeer(bucket *Bucket) *Peer {
	if bucket == nil {
		return nil
	}

	currentEntries := make(map[uint64]struct{}, len(bucket.Entries))

	for _, entry := range bucket.Entries {
		if entry == nil {
			continue
		}

		currentEntries[entry.ID] = struct{}{}
	}

	eligible := make(PeerSet, 0, len(bucket.Candidates))
	threshold := rt.bucketSecurityThreshold(bucket.Index)

	for _, candidate := range bucket.Candidates {
		if candidate == nil {
			continue
		}

		if _, exists := currentEntries[candidate.ID]; exists {
			continue
		}

		if candidate.RTT < threshold {
			continue
		}

		eligible = append(eligible, candidate)
	}

	if len(eligible) == 0 {
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

	eligible.SortByID()

	return eligible[rt.random.Intn(len(eligible))]
}

func (rt *RoutingTable) peerRTT(peerID uint64) (float64, bool) {
	bucketIndex := rt.bucketIndexFor(peerID)

	if bucketIndex < 0 || bucketIndex >= len(rt.buckets) {
		return 0, false
	}

	bucket := rt.buckets[bucketIndex]

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

func (rt *RoutingTable) bucketSecurityThreshold(bucketIndex int) float64 {
	if bucketIndex >= 0 && bucketIndex < len(rt.bucketSecurityThresholds) {
		return rt.bucketSecurityThresholds[bucketIndex]
	}

	return rt.securityThreshold
}

func (rt *RoutingTable) lookupNodes(target uint64, limit int) PeerSet {
	if limit <= 0 {
		return nil
	}

	shortlist := rt.closestLookupPeers(target)
	seen := map[uint64]struct{}{rt.nodeID: {}}
	discovered := map[uint64]*Peer{
		rt.nodeID: {ID: rt.nodeID, Node: rt.node},
	}

	for {
		batch := shortlist.NextBatch(seen, rt.lookupParallelism)

		if len(batch) == 0 {
			break
		}

		for _, peer := range batch {
			seen[peer.ID] = struct{}{}

			if peer.Node == nil {
				continue
			}

			discovered[peer.ID] = peer
			rt.observePeerQuery(peer)
			shortlist = shortlist.Merge(
				peer.Node.routing.closestLookupPeers(target), target,
			)
		}
	}

	nodes := make(PeerSet, 0, len(discovered))

	for _, candidate := range discovered {
		nodes = append(nodes, candidate)
	}

	nodes.SortByDistance(target)

	if len(nodes) > limit {
		nodes = nodes[:limit]
	}

	return nodes
}
