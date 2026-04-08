package kadabra

import (
	"maps"
	"math/bits"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
RoutingTable owns Kademlia-style XOR routing buckets and the local
record store. Peer registration, affinity-based lookup, and
trie-cluster management all live here.

Record dedupe shards use copy-on-write maps of *SequenceRecord so a
successful admission clones only pointers (~8 bytes per key) instead
of copying whole 1KB primitive.Value bodies on every maps.Clone.

Sharding into 256 slices caps clone work per mutation to a single
shard while keeping lookup lock-free via atomic.Pointer swaps.
*/
type recordSnapshot struct {
	m map[uint64]*SequenceRecord
}

type recordShard struct {
	ptr atomic.Pointer[recordSnapshot]
}

const recordShardCount = 256

type RoutingTable struct {
	nodeID      uint64
	bucketSize  int
	routingBits int
	buckets     []*Bucket
	random      *rand.Rand
	node        *Node
	backend     *compute.Backend
	shards      [recordShardCount]recordShard
}

/*
NewRoutingTable constructs a routing table for the given node.
*/
func NewRoutingTable(node *Node) *RoutingTable {
	rt := &RoutingTable{
		nodeID:      node.ID,
		bucketSize:  core.Cfg.Kadabra.BucketSize,
		routingBits: core.Cfg.Kadabra.Bits,
		buckets:     make([]*Bucket, core.Cfg.Kadabra.Bits),
		node:        node,
		random:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for idx := range rt.shards {
		rt.shards[idx].ptr.Store(&recordSnapshot{m: make(map[uint64]*SequenceRecord)})
	}

	for idx := range rt.buckets {
		rt.buckets[idx] = &Bucket{Index: idx}
		rt.buckets[idx].state.Store(newBucketState())
	}

	return rt
}

/*
AddPeer registers a peer as a routing candidate, seeding the bucket
when capacity remains. Uses insertion sort for the single new element
instead of a full sort inside the CAS loop.
*/
func (rt *RoutingTable) AddPeer(peer *Node, rtt float64) {
	if peer == nil || peer.ID == rt.nodeID {
		return
	}

	idx := IndexFor(rt.nodeID, peer.ID, rt.routingBits)

	if idx < 0 || idx >= len(rt.buckets) {
		return
	}

	rt.buckets[idx].CAS(func(st *bucketState) {
		centroid := primitive.AffinityForNodeID(peer.ID)

		if existing := st.Candidates[peer.ID]; existing != nil {
			existing.Node = peer
			existing.RTT = rtt

			if existing.Affinity == nil {
				existing.Affinity = centroid
			}

			for _, entry := range st.Entries {
				if entry != nil && entry.ID == peer.ID {
					entry.Node = peer
					entry.RTT = rtt

					if entry.Affinity == nil {
						entry.Affinity = centroid
					}
				}
			}

			return
		}

		candidate := &Peer{
			ID:       peer.ID,
			Affinity: centroid,
			Node:     peer,
			RTT:      rtt,
		}

		st.Candidates[peer.ID] = candidate

		if len(st.Entries) < rt.bucketSize {
			st.Entries = append(st.Entries, candidate)

			for pos := len(st.Entries) - 1; pos > 0 && st.Entries[pos].ID < st.Entries[pos-1].ID; pos-- {
				st.Entries[pos], st.Entries[pos-1] = st.Entries[pos-1], st.Entries[pos]
			}

			viz.DefaultBus.Publish(viz.PeerAdded(rt.nodeID, peer.ID, idx))
		}
	})

	viz.DefaultBus.Publish(viz.PeerLatency(rt.nodeID, peer.ID, rtt))
}

/*
Closest returns up to limit nodes from the routing table sorted by
affinity Hamming distance to target. The owning node is always
included as a candidate.

Distances are computed via popcount XOR on the 512-bit affinity
vectors. Bucket-sorted in O(N) using the bounded [0,512] distance
range — no comparison sort needed.
*/
func (rt *RoutingTable) Closest(
	target *primitive.Affinity, limit int,
) []*Node {
	if limit <= 0 || target == nil {
		return nil
	}

	type candidate struct {
		node *Node
		dist int
	}

	candidates := make([]candidate, 0, 256)
	targetVec := target.Vector()

	affinityDist := func(aff *primitive.Affinity) int {
		if aff == nil {
			return primitive.AffinityBits
		}

		vec := aff.Vector()
		dist := 0

		for wordIdx := range primitive.AffinityWords {
			aWord := targetVec[wordIdx] ^ vec[wordIdx]

			if wordIdx == primitive.AffinityWords-1 {
				aWord &= primitive.AffinityLastWordMask
			}

			dist += bits.OnesCount64(aWord)
		}

		return dist
	}

	candidates = append(candidates, candidate{
		node: rt.node,
		dist: primitive.AffinityBits,
	})

	for _, bucket := range rt.buckets {
		if bucket == nil {
			continue
		}

		st := bucket.state.Load()

		if st == nil {
			continue
		}

		for _, entry := range st.Entries {
			if entry != nil && entry.Node != nil && entry.ID != rt.nodeID {
				candidates = append(candidates, candidate{
					node: entry.Node,
					dist: affinityDist(entry.Affinity),
				})
			}
		}
	}

	count := len(candidates)

	if count == 0 {
		return nil
	}

	if limit > count {
		limit = count
	}

	var bucketCounts [primitive.AffinityBits + 1]int
	for idx := range candidates {
		bucketCounts[candidates[idx].dist]++
	}

	var bucketOffsets [primitive.AffinityBits + 1]int
	offset := 0

	for dist := range bucketOffsets {
		bucketOffsets[dist] = offset
		offset += bucketCounts[dist]
	}

	sorted := make([]*Node, count)

	for idx := range candidates {
		dist := candidates[idx].dist
		sorted[bucketOffsets[dist]] = candidates[idx].node
		bucketOffsets[dist]++
	}

	return sorted[:limit]
}

/*
claimRecordIfNew installs record into the sharded record index when Key is
absent. It returns true on first admission and false when Key already
exists (duplicate primary publish or replica redelivery).

The trie insert path must gate on this so at-least-once delivery does not
double-count MarkovTrie statistics or re-fire trie visualization for the
same logical record.
*/
func (rt *RoutingTable) claimRecordIfNew(record SequenceRecord) bool {
	if rt == nil {
		return false
	}

	shardIdx := int(record.Key) & (recordShardCount - 1)

	for {
		old := rt.shards[shardIdx].ptr.Load()
		var base map[uint64]*SequenceRecord

		if old != nil && old.m != nil {
			if _, exists := old.m[record.Key]; exists {
				return false
			}

			base = maps.Clone(old.m)
		} else {
			base = make(map[uint64]*SequenceRecord)
		}

		recordClone := new(SequenceRecord)
		*recordClone = record
		base[record.Key] = recordClone
		next := &recordSnapshot{m: base}

		if rt.shards[shardIdx].ptr.CompareAndSwap(old, next) {
			return true
		}
	}
}

/*
releaseRecordKey removes a Key from the sharded index after a failed
downstream step (e.g. trie spawn error) so admission does not leave a
permanent tombstone blocking a later successful insert.
*/
func (rt *RoutingTable) releaseRecordKey(key uint64) {
	if rt == nil {
		return
	}

	shardIdx := int(key) & (recordShardCount - 1)

	for {
		old := rt.shards[shardIdx].ptr.Load()

		if old == nil || old.m == nil {
			return
		}

		if _, exists := old.m[key]; !exists {
			return
		}

		base := maps.Clone(old.m)
		delete(base, key)
		next := &recordSnapshot{m: base}

		if rt.shards[shardIdx].ptr.CompareAndSwap(old, next) {
			return
		}
	}
}
