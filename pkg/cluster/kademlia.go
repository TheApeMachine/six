package cluster

import (
	"context"
	"math/bits"
	"sort"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

// NodeID is a 64-bit identifier derived from a Value's affinity word.
type NodeID uint64

const (
	IDBits = 64
)

// xorDist returns the XOR distance between two NodeIDs.
func xorDist(a, b NodeID) uint64 {
	return uint64(a ^ b)
}

// bucketIndex returns which k-bucket [0, IDBits) a remote ID belongs to,
// based on the length of the common prefix with the local ID.
// Bucket 0 = distance >= 2^63 (differs in MSB), bucket 63 = closest.
func bucketIndex(local, remote NodeID) int {
	d := xorDist(local, remote)
	if d == 0 {
		return int(core.Cfg.ControlPlane.Affinity.Bits - 1)
	}
	return bits.LeadingZeros64(d)
}

// entry holds a Value and its NodeID (affinity) within a k-bucket.
type entry struct {
	id    NodeID
	value *primitive.Value
}

// kBucket is a fixed-capacity bucket of the K closest seen nodes at a given
// prefix distance. It also owns an LSM for tokenID → Value bitmap.
type kBucket struct {
	mu      sync.RWMutex
	entries []entry // ordered: least-recently seen at front
	lsm     *store.SpatialIndex
}

func newKBucket() *kBucket {
	return &kBucket{
		entries: make([]entry, 0, core.Cfg.ControlPlane.K),
		lsm:     store.NewSpatialIndex(),
	}
}

// touch inserts or refreshes a Value in the bucket. If the bucket is full
// the oldest entry is evicted using in-place copy to preserve capacity.
func (bucket *kBucket) touch(id NodeID, value *primitive.Value) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Check if already present — move to tail (most-recently seen).
	for i, e := range bucket.entries {
		if e.id == id {
			copy(bucket.entries[i:], bucket.entries[i+1:])
			bucket.entries[len(bucket.entries)-1] = entry{id: id, value: value}
			return
		}
	}

	// Evict oldest in-place to preserve backing array capacity.
	if len(bucket.entries) >= core.Cfg.ControlPlane.K {
		copy(bucket.entries, bucket.entries[1:])
		bucket.entries = bucket.entries[:core.Cfg.ControlPlane.K-1]
	}

	bucket.entries = append(bucket.entries, entry{id: id, value: value})
	bucket.lsm.InsertBatch(tokenIDsFor(value), *value)
}

// closest returns up to k entries sorted by XOR distance to target.
func (bucket *kBucket) closest(target NodeID, k int) []entry {
	bucket.mu.RLock()
	defer bucket.mu.RUnlock()

	out := make([]entry, len(bucket.entries))
	copy(out, bucket.entries)
	sort.Slice(out, func(i, j int) bool {
		return xorDist(out[i].id, target) < xorDist(out[j].id, target)
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

/*
RoutingTable is the Kademlia routing table: IDBits k-buckets
indexed by prefix distance from the local node ID.
*/
type RoutingTable struct {
	mu           sync.RWMutex
	local        NodeID
	bootstrapped bool
	buckets      [IDBits]*kBucket
}

/*
NewRoutingTable creates a routing table for the given local NodeID.
*/
func NewRoutingTable(local NodeID) *RoutingTable {
	rt := &RoutingTable{local: local}

	for i := range rt.buckets {
		rt.buckets[i] = newKBucket()
	}

	return rt
}

/*
SetLocal sets the local NodeID in a thread-safe manner
and marks the table as bootstrapped so NodeID(0) is
never mistaken for an uninitialized state.
*/
func (rt *RoutingTable) SetLocal(id NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.local = id
	rt.bootstrapped = true
}

/*
isBootstrapped reports whether the local ID has been set.
*/
func (rt *RoutingTable) isBootstrapped() bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.bootstrapped
}

/*
Insert adds or refreshes a Value in the appropriate k-bucket.
*/
func (rt *RoutingTable) Insert(value *primitive.Value) {
	id := NodeID(value[affinityWordIndex()])
	rt.mu.RLock()
	local := rt.local
	rt.mu.RUnlock()

	if id == local {
		return
	}

	idx := bucketIndex(local, id)
	rt.buckets[idx].touch(id, value)
}

/*
FindClosest returns up to k Values whose NodeIDs are closest
to target. Expands outward from the target bucket one bucket
at a time, only visiting each bucket index once.
*/
func (rt *RoutingTable) FindClosest(target NodeID, k int) []entry {
	rt.mu.RLock()
	local := rt.local
	rt.mu.RUnlock()
	idx := bucketIndex(local, target)

	var candidates []entry
	candidates = append(candidates, rt.buckets[idx].closest(target, k)...)

	for radius := 1; radius < IDBits && len(candidates) < k; radius++ {
		if lo := idx - radius; lo >= 0 {
			candidates = append(
				candidates,
				rt.buckets[lo].closest(target, k)...,
			)
		}

		if hi := idx + radius; hi < IDBits {
			candidates = append(
				candidates,
				rt.buckets[hi].closest(target, k)...,
			)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return xorDist(
			candidates[i].id, target,
		) < xorDist(
			candidates[j].id, target,
		)
	})

	if len(candidates) > k {
		candidates = candidates[:k]
	}

	return candidates
}

/*
FindNode is a stub for the iterative Kademlia FIND_NODE lookup.
In a single-node deployment this is equivalent to FindClosest.
TODO: Replace with network RPC queries to candidate nodes via UniConn.
*/
func (rt *RoutingTable) FindNode(_ context.Context, target NodeID) []entry {
	return rt.FindClosest(target, core.Cfg.ControlPlane.K)
}

/*
Store places a Value into the routing table.
*/
func (rt *RoutingTable) Store(value *primitive.Value) {
	rt.Insert(value)
}

/*
tokenIDsFor extracts the non-zero token words from a Value's token region.
*/
func tokenIDsFor(value *primitive.Value) []uint64 {
	tokenIndex := core.Cfg.Value.Region.Tokens.Start
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	ids := make([]uint64, 0, tokenWords)

	for i := 0; i < tokenWords; i++ {
		if w := value[tokenIndex+i]; w != 0 {
			ids = append(ids, w)
		}
	}

	return ids
}

/*
affinityWordIndex returns the word index of the 
affinity register from the Value's region.
*/
func affinityWordIndex() int {
	return core.Cfg.Value.Region.Affinity.Start
}
