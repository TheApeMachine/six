package cluster

import (
	"context"
	"math/bits"
	"sort"
	"sync"

	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

const (
	// K is the maximum number of Values per k-bucket.
	K = 20
	// IDBits is the width of the NodeID space (matches affinity word).
	IDBits = 64
	// Alpha is the concurrency factor for iterative lookups.
	Alpha = 3
)

// NodeID is a 64-bit identifier derived from a Value's affinity word.
type NodeID uint64

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
		return IDBits - 1
	}
	return bits.LeadingZeros64(d)
}

// entry holds a Value and its NodeID (affinity) within a k-bucket.
type entry struct {
	id    NodeID
	value primitive.Value
}

// kBucket is a fixed-capacity bucket of the K closest seen nodes
// at a given prefix distance. It also owns an LSM for fast spatial queries
// within its prefix range.
type kBucket struct {
	mu      sync.RWMutex
	entries []entry // ordered: least-recently seen at front
	lsm     *store.SpatialIndex
}

func newKBucket() *kBucket {
	return &kBucket{
		entries: make([]entry, 0, K),
		lsm:     store.NewSpatialIndex(),
	}
}

// touch inserts or refreshes a Value in the bucket. If the bucket is full
// the oldest entry is evicted using in-place copy to preserve capacity.
func (b *kBucket) touch(id NodeID, value primitive.Value) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already present — move to tail (most-recently seen).
	for i, e := range b.entries {
		if e.id == id {
			copy(b.entries[i:], b.entries[i+1:])
			b.entries[len(b.entries)-1] = entry{id: id, value: value}
			// LSM does not deduplicate; only insert on new entries (see below).
			return
		}
	}

	// Evict oldest in-place to preserve backing array capacity.
	if len(b.entries) >= K {
		copy(b.entries, b.entries[1:])
		b.entries = b.entries[:K-1]
	}

	b.entries = append(b.entries, entry{id: id, value: value})
	b.lsm.InsertBatch(tokenIDsFor(value), [primitive.Words]uint64(value))
}

// closest returns up to k entries sorted by XOR distance to target.
func (b *kBucket) closest(target NodeID, k int) []entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]entry, len(b.entries))
	copy(out, b.entries)
	sort.Slice(out, func(i, j int) bool {
		return xorDist(out[i].id, target) < xorDist(out[j].id, target)
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

// queryHamming returns Value frames from this bucket's LSM within
// maxDistance Hamming distance of targetAffinity.
func (b *kBucket) queryHamming(targetAffinity uint64, maxDistance int) [][primitive.Words]uint64 {
	return b.lsm.QueryHamming(targetAffinity, maxDistance)
}

// RoutingTable is the Kademlia routing table: IDBits k-buckets indexed by
// prefix distance from the local node ID.
type RoutingTable struct {
	local   NodeID
	buckets [IDBits]*kBucket
}

// NewRoutingTable creates a routing table for the given local NodeID.
func NewRoutingTable(local NodeID) *RoutingTable {
	rt := &RoutingTable{local: local}
	for i := range rt.buckets {
		rt.buckets[i] = newKBucket()
	}
	return rt
}

// Insert adds or refreshes a Value in the appropriate k-bucket.
func (rt *RoutingTable) Insert(value primitive.Value) {
	id := NodeID(value[affinityWordIndex()])
	if id == rt.local {
		return
	}
	idx := bucketIndex(rt.local, id)
	rt.buckets[idx].touch(id, value)
}

// FindClosest returns up to k Values whose NodeIDs are closest to target.
// Expands outward from the target bucket one bucket at a time, only visiting
// each bucket index once.
func (rt *RoutingTable) FindClosest(target NodeID, k int) []entry {
	idx := bucketIndex(rt.local, target)
	var candidates []entry

	candidates = append(candidates, rt.buckets[idx].closest(target, k)...)

	for radius := 1; radius < IDBits && len(candidates) < k; radius++ {
		if lo := idx - radius; lo >= 0 {
			candidates = append(candidates, rt.buckets[lo].closest(target, k)...)
		}
		if hi := idx + radius; hi < IDBits {
			candidates = append(candidates, rt.buckets[hi].closest(target, k)...)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return xorDist(candidates[i].id, target) < xorDist(candidates[j].id, target)
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates
}

// QueryHamming walks all buckets and returns every Value frame within
// maxDistance Hamming distance of targetAffinity. Does not break early —
// Hamming distance and XOR prefix distance do not map 1:1 so all buckets
// must be checked to avoid silently discarding valid matches.
func (rt *RoutingTable) QueryHamming(targetAffinity uint64, maxDistance int) [][primitive.Words]uint64 {
	var results [][primitive.Words]uint64
	for i := range rt.buckets {
		results = append(results, rt.buckets[i].queryHamming(targetAffinity, maxDistance)...)
	}
	return results
}

// FindNode is a stub for the iterative Kademlia FIND_NODE lookup.
// In a single-node deployment this is equivalent to FindClosest.
// TODO: Replace with network RPC queries to candidate nodes via UniConn.
func (rt *RoutingTable) FindNode(_ context.Context, target NodeID) []entry {
	return rt.FindClosest(target, K)
}

// Store places a Value into the routing table.
func (rt *RoutingTable) Store(value primitive.Value) {
	rt.Insert(value)
}

// tokenIDsFor extracts the non-zero token words from a Value's token region
// (words 0-56).
func tokenIDsFor(value primitive.Value) []uint64 {
	const tokenIndex = 0
	const tokenWords = 57
	ids := make([]uint64, 0, tokenWords)
	for i := 0; i < tokenWords; i++ {
		if w := value[tokenIndex+i]; w != 0 {
			ids = append(ids, w)
		}
	}
	return ids
}

// affinityWordIndex returns the word index of the affinity register (word 63).
func affinityWordIndex() int {
	return 63
}
