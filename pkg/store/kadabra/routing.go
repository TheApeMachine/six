package kadabra

import (
	"math/rand"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
RoutingTable owns Kademlia-style XOR routing buckets and the local
record store. Peer registration, affinity-based lookup, and
trie-cluster management all live here.

Records use an RWMutex instead of copy-on-write: at 50M+ values,
maps.Clone per insert is O(N) and saturates the GC. The mutex
write path is O(1) amortized with a fast RLock read path for
duplicate detection.
*/
/*
recordSnapshot is an immutable map swapped atomically via CAS.
Sharded into 256 slices so each clone is ~1/256th of total records,
keeping copy cost bounded while remaining fully lock-free.
*/
type recordSnapshot struct {
	m map[uint64]SequenceRecord
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
func NewRoutingTable(node *Node, backend *compute.Backend) *RoutingTable {
	rt := &RoutingTable{
		nodeID:      node.ID,
		bucketSize:  core.Cfg.Kadabra.BucketSize,
		routingBits: core.Cfg.Kadabra.Bits,
		buckets:     make([]*Bucket, core.Cfg.Kadabra.Bits),
		node:        node,
		backend:     backend,
		random:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for idx := range rt.shards {
		rt.shards[idx].ptr.Store(&recordSnapshot{m: make(map[uint64]SequenceRecord)})
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
		if existing := st.Candidates[peer.ID]; existing != nil {
			existing.Node = peer
			existing.RTT = rtt

			for _, entry := range st.Entries {
				if entry != nil && entry.ID == peer.ID {
					entry.Node = peer
					entry.RTT = rtt
				}
			}

			return
		}

		candidate := &Peer{ID: peer.ID, Node: peer, RTT: rtt}
		st.Candidates[peer.ID] = candidate

		if len(st.Entries) < rt.bucketSize {
			st.Entries = append(st.Entries, candidate)

			for pos := len(st.Entries) - 1; pos > 0 && st.Entries[pos].ID < st.Entries[pos-1].ID; pos-- {
				st.Entries[pos], st.Entries[pos-1] = st.Entries[pos-1], st.Entries[pos]
			}
		}
	})
}

/*
Closest returns up to limit nodes from the routing table sorted by
affinity Hamming distance. The owning node is always included.

Kademlia XOR buckets are mutually exclusive so deduplication is
unnecessary. Distances are computed in a single SIMD batch pass,
then bucket-sorted in O(N) using the bounded [0,512] distance range
instead of comparison-sorting in O(N log N).
*/
func (rt *RoutingTable) Closest(
	target *primitive.Affinity, limit int,
) []*Node {
	if limit <= 0 {
		return nil
	}

	nodes := make([]*Node, 0, 256)
	nodes = append(nodes, rt.node)

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
				nodes = append(nodes, entry.Node)
			}
		}
	}

	count := len(nodes)
	vectors := make([][primitive.AffinityWords]uint64, count)

	for idx, node := range nodes {
		if node.Affinity != nil {
			vectors[idx] = node.Affinity.Vector()
		}
	}

	distances := make([]int32, count)

	queryVec := target.Vector()
	udist := make([]uint32, count)

	rt.backend.BatchDistances(
		unsafe.Pointer(&queryVec[0]),
		unsafe.Pointer(&vectors[0][0]),
		count,
		udist,
	)

	for idx := range udist {
		distances[idx] = int32(udist[idx])
	}

	var bucketHeads [513]int32
	var bucketTails [513]int32

	for idx := range distances {
		bucketHeads[distances[idx]]++
	}

	var offset int32

	for dist := range bucketHeads {
		n := bucketHeads[dist]
		bucketHeads[dist] = offset
		bucketTails[dist] = offset
		offset += n
	}

	sorted := make([]*Node, count)

	for idx, dist := range distances {
		pos := bucketTails[dist]
		sorted[pos] = nodes[idx]
		bucketTails[dist]++
	}

	if limit > count {
		limit = count
	}

	return sorted[:limit]
}
