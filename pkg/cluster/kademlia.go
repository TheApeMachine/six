package cluster

import (
	"context"
	"math/bits"
	"sort"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
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
	bitsCfg := core.Cfg.ControlPlane.Affinity.Bits

	if bitsCfg == 0 {
		bitsCfg = 1
	}

	d := xorDist(local, remote)

	var idx int

	if d == 0 {
		idx = int(bitsCfg) - 1
	} else {
		idx = bits.LeadingZeros64(d)
	}

	if idx < 0 {
		idx = 0
	}

	if idx >= IDBits {
		idx = IDBits - 1
	}

	return idx
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

// insert adds or refreshes a node in the bucket with LRU semantics.
// Existing entries are moved to the tail (most-recently seen). When
// the bucket is full the least-recently seen entry (index 0) is evicted.
// TODO: implement ping-based eviction per Kademlia spec for multi-node.
func (bucket *kBucket) insert(id NodeID, value *primitive.Value) {
	if value == nil {
		return
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refresh existing entry: shift it out and re-append at tail.
	for i, e := range bucket.entries {
		if e.id == id {
			copy(bucket.entries[i:], bucket.entries[i+1:])
			bucket.entries[len(bucket.entries)-1] = entry{id: id, value: value}
			return
		}
	}

	limit := core.Cfg.ControlPlane.K
	if limit <= 0 {
		limit = 20
	}

	if len(bucket.entries) < limit {
		bucket.entries = append(bucket.entries, entry{id: id, value: value})
		return
	}

	// Evict least-recently seen (index 0) in place — zero allocations.
	copy(bucket.entries, bucket.entries[1:])
	bucket.entries[len(bucket.entries)-1] = entry{id: id, value: value}
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

The LSM spatial index is ALWAYS written to — even for the local node's
own data — because FrameByValueID, LookupKeysByValue, and DecodeTokenIDs
all depend on it. Without this, the first value inserted during bootstrap
(which becomes the local ID via ControlPlane) would be silently lost.

Routing-table peer entries are only populated for non-local IDs, which
preserves the Kademlia invariant that a node does not route to itself.
*/
func (rt *RoutingTable) Insert(key uint64, value *primitive.Value) {
	if value == nil {
		return
	}

	rt.mu.RLock()
	local := rt.local
	rt.mu.RUnlock()

	id := NodeID(key)
	idx := bucketIndex(local, id)
	bucket := rt.buckets[idx]

	// Always index into storage — this is how Values are found by ID,
	// affinity, and token lookup regardless of who owns them.
	bucket.lsm.InsertBatch(insertTokenKeysForValue(value), *value)

	// But never add self to peer routing entries.
	if id != local {
		bucket.insert(id, value)
	}

	errnie.Trace("cluster.kademlia.Insert", "key", key, "bucket", idx)
}

/*
insertTokenKeysForValue chooses LSM inverted-index keys for a frame.

NewValue persists per-byte affine TokenIDs (constructor Tokenize output). Those
are the same identifiers DecodeTokenIDs expects. The in-band token region holds
superimposed composite words, so value.TokenIDs() is the wrong key space for
postings used to rebuild plaintext.
*/
func insertTokenKeysForValue(value *primitive.Value) []uint64 {
	if value == nil {
		return nil
	}

	valueID := value.GetWord(core.Cfg.Value.Region.ID.Start)
	if valueID != 0 {
		if keys := primitive.ValueTokenIDsForLookup(valueID); len(keys) > 0 {
			return keys
		}
	}

	return value.TokenIDs()
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

	errnie.Trace("cluster.kademlia.FindClosest", "target", target, "candidates", candidates)
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
affinityWordIndex returns the word index of the
affinity register from the Value's region.
*/
func affinityWordIndex() int {
	return core.Cfg.Value.Region.Affinity.Start
}
