package kadabra

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
	"strings"
)

const (
	dhtIDBits                  = 64
	defaultKadabraBucketSize   = 20
	defaultKadabraReplication  = 3
	defaultKadabraAlpha        = 3
	defaultKadabraEpochQueries = 100
)

/*
SequenceRecord is the replicated DHT value stored at Kadabra nodes.
*/
type SequenceRecord struct {
	Key       uint64
	Sequence  string
	Label     string
	Publisher NodeID
}

/*
LookupTrace records the nodes queried during a lookup.
*/
type LookupTrace struct {
	Key     uint64
	Nodes   []NodeID
	Latency float64
	Found   bool
}

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

/*
HashSequenceRecord derives the DHT key for one replicated sequence record.
*/
func HashSequenceRecord(sequence string, label string) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(label))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(sequence))
	return hasher.Sum64()
}

func (node *KadabraNode) closestLookupPeers(target NodeID) []*kadabraPeer {
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

	return dedupePeers(peers)
}

func (node *KadabraNode) observePeerQuery(peer *kadabraPeer) {
	if peer == nil {
		return
	}

	bucket := node.buckets[kadabraBucketIndex(node.ID, peer.ID)]
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
}

func (node *KadabraNode) finishEpoch(bucket *kadabraBucket) {
	if bucket == nil || bucket.QueryCount == 0 {
		return
	}

	penalty := node.epochPenalty(bucket)
	currentScores := make(map[NodeID]float64, len(bucket.Entries))
	for _, entry := range bucket.Entries {
		sample := bucket.Samples[entry.ID]
		usedQueries := 0
		latencyTotal := 0.0
		if sample != nil {
			usedQueries = sample.Queries
			latencyTotal = sample.LatencyTotal
		}

		currentScores[entry.ID] = -(latencyTotal + float64(bucket.QueryCount-usedQueries)*penalty)
	}

	currentBucketScore := averagePeerScores(currentScores)
	if bucket.ExploreNext {
		bucket.PreviousEntries = clonePeers(bucket.Entries)
		bucket.PreviousScore = currentBucketScore

		replacement := node.selectExplorationPeer(bucket)
		if replacement != nil {
			worstPeerID := worstPeerScore(currentScores)
			for entryIndex, entry := range bucket.Entries {
				if entry.ID != worstPeerID {
					continue
				}

				bucket.Entries[entryIndex] = clonePeer(replacement)
				break
			}
			sortPeersByID(bucket.Entries)
		}

		bucket.ExploreNext = false
	} else {
		if currentBucketScore > bucket.PreviousScore || len(bucket.PreviousEntries) == 0 {
			bucket.PreviousEntries = clonePeers(bucket.Entries)
			bucket.PreviousScore = currentBucketScore
		} else {
			bucket.Entries = clonePeers(bucket.PreviousEntries)
		}

		bucket.ExploreNext = true
	}

	bucket.QueryCount = 0
	bucket.Samples = make(map[NodeID]*kadabraPeerSample)
}

func (node *KadabraNode) selectExplorationPeer(bucket *kadabraBucket) *kadabraPeer {
	if bucket == nil {
		return nil
	}

	currentEntries := make(map[NodeID]struct{}, len(bucket.Entries))
	for _, entry := range bucket.Entries {
		currentEntries[entry.ID] = struct{}{}
	}

	eligible := make([]*kadabraPeer, 0, len(bucket.Candidates))
	securityThreshold := node.bucketSecurityThreshold(bucket.Index)
	for _, candidate := range bucket.Candidates {
		if _, exists := currentEntries[candidate.ID]; exists {
			continue
		}

		if candidate.RTT < securityThreshold {
			continue
		}

		eligible = append(eligible, candidate)
	}

	if len(eligible) == 0 {
		for _, candidate := range bucket.Candidates {
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

func (node *KadabraNode) epochPenalty(bucket *kadabraBucket) float64 {
	if node.Penalty > 0 {
		return node.Penalty
	}

	totalLatency := 0.0
	totalQueries := 0
	for _, sample := range bucket.Samples {
		totalLatency += sample.LatencyTotal
		totalQueries += sample.Queries
	}

	if totalQueries == 0 {
		return 1
	}

	return totalLatency/float64(totalQueries) + 1
}

func (node *KadabraNode) bucketSecurityThreshold(bucketIndex int) float64 {
	if bucketIndex >= 0 && bucketIndex < len(node.BucketSecurityThresholds) {
		return node.BucketSecurityThresholds[bucketIndex]
	}

	return node.SecurityThreshold
}

func (node *KadabraNode) peerRTT(peerID NodeID) float64 {
	bucket := node.buckets[kadabraBucketIndex(node.ID, peerID)]
	if bucket != nil {
		if candidate := bucket.Candidates[peerID]; candidate != nil {
			return candidate.RTT
		}
	}

	return 0
}

func averagePeerScores(scores map[NodeID]float64) float64 {
	if len(scores) == 0 {
		return math.Inf(-1)
	}

	total := 0.0
	for _, score := range scores {
		total += score
	}

	return total / float64(len(scores))
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
		merged[peer.ID] = peer
	}

	for _, peer := range right {
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
		out = append(out, peer)
	}

	sort.Slice(out, func(
		leftIndex int, rightIndex int,
	) bool {
		leftPeer := out[leftIndex]
		rightPeer := out[rightIndex]
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
		return peers[leftIndex].ID < peers[rightIndex].ID
	})
}

func xorDistance(left NodeID, right NodeID) uint64 {
	return uint64(left ^ right)
}

func kadabraBucketIndex(local NodeID, remote NodeID) int {
	distance := xorDistance(local, remote)
	if distance == 0 {
		return dhtIDBits - 1
	}

	index := bits.LeadingZeros64(distance)

	if index >= dhtIDBits {
		return dhtIDBits - 1
	}

	return index
}

/*
NodeIDFromBytes derives a 64-bit node identifier from up to eight bytes.
*/
func NodeIDFromBytes(value []byte) NodeID {
	var buffer [8]byte
	copy(buffer[:], value)
	return NodeID(binary.BigEndian.Uint64(buffer[:]))
}

/*
NodeIDFromString hashes an arbitrary string into a 64-bit node identifier.
*/
func NodeIDFromString(value string) NodeID {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(value)))
	return NodeID(hasher.Sum64())
}
