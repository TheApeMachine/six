package kadabra

import (
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/viz"
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

			viz.DefaultBus.Publish(viz.PeerAdded(rt.nodeID, peer.ID, idx))
		}
	})

	viz.DefaultBus.Publish(viz.PeerLatency(rt.nodeID, peer.ID, rtt))
}

// /*
// Closest returns up to limit nodes from the routing table sorted by
// affinity Hamming distance. The owning node is always included.

// Distances are computed through the programmer → backend → kernel
// path. The programmer compiles a batch distance Value and the kernel
// writes uint32 results into signals. Bucket-sorted in O(N) using
// the bounded [0,512] distance range.
// */
// func (rt *RoutingTable) Closest(
// 	target *primitive.Affinity, limit int,
// ) []*Node {
// 	if limit <= 0 {
// 		return nil
// 	}

// 	nodes := make([]*Node, 0, 256)
// 	nodes = append(nodes, rt.node)

// 	for _, bucket := range rt.buckets {
// 		if bucket == nil {
// 			continue
// 		}

// 		st := bucket.state.Load()

// 		if st == nil {
// 			continue
// 		}

// 		for _, entry := range st.Entries {
// 			if entry != nil && entry.Node != nil && entry.ID != rt.nodeID {
// 				nodes = append(nodes, entry.Node)
// 			}
// 		}
// 	}

// 	count := len(nodes)

// 	// Collect candidate vectors for the programmer.
// 	candidates := make([][]uint64, count)

// 	for idx, node := range nodes {
// 		if node.Affinity != nil {
// 			vec := node.Affinity.Vector()
// 			candidates[idx] = vec[:]
// 		} else {
// 			candidates[idx] = make([]uint64, primitive.AffinityWords)
// 		}
// 	}

// 	// Bucket sort by distance (bounded [0,512]).
// 	var bucketHeads [513]int32
// 	var bucketTails [513]int32

// 	var offset int32

// 	for dist := range bucketHeads {
// 		n := bucketHeads[dist]
// 		bucketHeads[dist] = offset
// 		bucketTails[dist] = offset
// 		offset += n
// 	}

// 	sorted := make([]*Node, count)

// 	if limit > count {
// 		limit = count
// 	}

// 	return sorted[:limit]
// }
