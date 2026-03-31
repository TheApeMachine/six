package store

import (
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"github.com/theapemachine/six/pkg/core"
)

const valueWords = 128

// ValueFrameBitmap builds a Roaring bitmap of all set bits in the 128×64-bit Value
// frame: bit i of the flat layout (word*64+bitIndex) is present iff it is set in v.
func ValueFrameBitmap(v [valueWords]uint64) *roaring64.Bitmap {
	b := roaring64.New()
	bitBase := 0
	for wi := range valueWords {
		w := v[wi]
		for i := range 64 {
			if w>>i&1 != 0 {
				b.Add(uint64(bitBase + i))
			}
		}
		bitBase += 64
	}
	return b
}

type valueStore struct {
	bitmap *roaring64.Bitmap
}

func newValueStore() *valueStore {
	return &valueStore{bitmap: roaring64.New()}
}

func mergeStores(a, b *valueStore) *valueStore {
	out := newValueStore()
	out.bitmap.Or(a.bitmap)
	out.bitmap.Or(b.bitmap)
	return out
}

// lsmLevel is a single sorted run in the LSM tree.
type lsmLevel struct {
	keys   []uint64
	stores []*valueStore
}

// SpatialIndex is an LSM-based index: TokenID (uint64 key) → Roaring bitmap of
// the Value frame. LookupKeysByValue scans keys for an exact bitmap match.
type SpatialIndex struct {
	mu       sync.RWMutex
	memtable map[uint64]*valueStore
	memSize  int
	levels   []*lsmLevel
}

var (
	defaultSpatialIndexMu sync.RWMutex
	defaultSpatialIndex   = NewSpatialIndex()
)

// NewSpatialIndex creates a new LSM index.
func NewSpatialIndex() *SpatialIndex {
	return &SpatialIndex{
		memtable: make(map[uint64]*valueStore),
	}
}

// DefaultSpatialIndex returns the shared repository-wide LSM.
func DefaultSpatialIndex() *SpatialIndex {
	defaultSpatialIndexMu.RLock()
	idx := defaultSpatialIndex
	defaultSpatialIndexMu.RUnlock()
	if idx != nil {
		return idx
	}
	defaultSpatialIndexMu.Lock()
	defer defaultSpatialIndexMu.Unlock()
	if defaultSpatialIndex == nil {
		defaultSpatialIndex = NewSpatialIndex()
	}
	return defaultSpatialIndex
}

// ResetDefaultSpatialIndex swaps the shared index for a fresh instance.
func ResetDefaultSpatialIndex() *SpatialIndex {
	defaultSpatialIndexMu.Lock()
	defer defaultSpatialIndexMu.Unlock()
	defaultSpatialIndex = NewSpatialIndex()
	return defaultSpatialIndex
}

// mergeLevels merges two sorted levels. Colliding TokenIDs merge their bitmaps (OR).
func mergeLevels(a, b *lsmLevel) *lsmLevel {
	out := &lsmLevel{
		keys:   make([]uint64, 0, len(a.keys)+len(b.keys)),
		stores: make([]*valueStore, 0, len(a.keys)+len(b.keys)),
	}
	i, j := 0, 0
	for i < len(a.keys) && j < len(b.keys) {
		ka, kb := a.keys[i], b.keys[j]
		switch {
		case ka < kb:
			out.keys = append(out.keys, ka)
			out.stores = append(out.stores, a.stores[i])
			i++
		case kb < ka:
			out.keys = append(out.keys, kb)
			out.stores = append(out.stores, b.stores[j])
			j++
		default:
			out.keys = append(out.keys, ka)
			out.stores = append(out.stores, mergeStores(a.stores[i], b.stores[j]))
			i++
			j++
		}
	}
	for ; i < len(a.keys); i++ {
		out.keys = append(out.keys, a.keys[i])
		out.stores = append(out.stores, a.stores[i])
	}
	for ; j < len(b.keys); j++ {
		out.keys = append(out.keys, b.keys[j])
		out.stores = append(out.stores, b.stores[j])
	}
	return out
}

// flushMemtable converts the memtable map into a sorted lsmLevel and merges it
// into the level hierarchy. Must be called with mu held.
func (idx *SpatialIndex) flushMemtable() {
	keys := make([]uint64, 0, len(idx.memtable))
	for k := range idx.memtable {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	lvl := &lsmLevel{
		keys:   keys,
		stores: make([]*valueStore, len(keys)),
	}
	for i, k := range keys {
		lvl.stores[i] = idx.memtable[k]
	}

	level := 0
	for level < len(idx.levels) {
		if idx.levels[level] == nil {
			break
		}
		lvl = mergeLevels(idx.levels[level], lvl)
		idx.levels[level] = nil
		level++
	}
	if level == len(idx.levels) {
		idx.levels = append(idx.levels, lvl)
	} else {
		idx.levels[level] = lvl
	}

	idx.memtable = make(map[uint64]*valueStore)
	idx.memSize = 0
}

// InsertBatch inserts a batch of (tokenID, value frame) pairs. The frame is encoded
// with ValueFrameBitmap; multiple inserts for the same key OR the bitmaps together.
func (idx *SpatialIndex) InsertBatch(tokenIDs []uint64, value [valueWords]uint64) {
	if len(tokenIDs) == 0 {
		return
	}

	bm := ValueFrameBitmap(value)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, tID := range tokenIDs {
		if _, exists := idx.memtable[tID]; !exists {
			idx.memtable[tID] = newValueStore()
		}
		idx.memtable[tID].bitmap.Or(bm)
		idx.memSize++
	}

	if idx.memSize >= core.Cfg.System.QueueSize {
		idx.flushMemtable()
	}
}

// InsertTokenSpan stores a contiguous token span for a single Value frame.
func (idx *SpatialIndex) InsertTokenSpan(tokenIDs []uint64, value [valueWords]uint64) {
	idx.InsertBatch(tokenIDs, value)
}

// Flush forces the memtable to be written to the level hierarchy.
func (idx *SpatialIndex) Flush() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.memSize > 0 || len(idx.memtable) > 0 {
		idx.flushMemtable()
	}
}

func (idx *SpatialIndex) exactLookupLocked(tokenID uint64) *roaring64.Bitmap {
	var toMerge []*roaring64.Bitmap

	if vs, ok := idx.memtable[tokenID]; ok {
		toMerge = append(toMerge, vs.bitmap)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		i := sort.Search(len(lvl.keys), func(j int) bool {
			return lvl.keys[j] >= tokenID
		})
		if i < len(lvl.keys) && lvl.keys[i] == tokenID {
			toMerge = append(toMerge, lvl.stores[i].bitmap)
		}
	}

	if len(toMerge) == 0 {
		return roaring64.New()
	}
	return roaring64.FastOr(toMerge...)
}

// ExactLookup returns the Roaring bitmap stored under tokenID (memtable and levels merged).
func (idx *SpatialIndex) ExactLookup(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.exactLookupLocked(tokenID)
}

// LookupKeysByValue returns every tokenID whose stored bitmap equals the encoding
// of v. This scans all keys in the index (memtable and flushed levels).
func (idx *SpatialIndex) LookupKeysByValue(v [valueWords]uint64) []uint64 {
	target := ValueFrameBitmap(v)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seen := make(map[uint64]struct{})
	var out []uint64
	try := func(tid uint64) {
		if _, ok := seen[tid]; ok {
			return
		}
		got := idx.exactLookupLocked(tid)
		if got.Equals(target) {
			seen[tid] = struct{}{}
			out = append(out, tid)
		}
	}

	for tid := range idx.memtable {
		try(tid)
	}
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, tid := range lvl.keys {
			try(tid)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// GetStats returns usage statistics for the index.
func (idx *SpatialIndex) GetStats() map[string]interface{} {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var totalKeys uint64
	var totalBytes uint64
	numLevels := 0

	if len(idx.memtable) > 0 {
		numLevels++
		for _, vs := range idx.memtable {
			totalKeys++
			totalBytes += vs.bitmap.GetSizeInBytes()
		}
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		numLevels++
		for _, vs := range lvl.stores {
			totalKeys++
			totalBytes += vs.bitmap.GetSizeInBytes()
		}
	}
	return map[string]interface{}{
		"total_keys":   totalKeys,
		"num_levels":   numLevels,
		"memory_bytes": totalBytes,
	}
}
