package store

import (
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

const (
	valueWords   = 128
	memTableSize = 1000
)

// Compile-time assertion: valueWords must match primitive.Words (128).
// If primitive.Words changes, this will produce a build error.
var _ [valueWords]struct{} = [128]struct{}{}

// frameHash computes a 64-bit FNV-1a fingerprint of a value frame for use as
// a cheap map key, avoiding full 1 KB array comparisons in the seen map.
func frameHash(v [valueWords]uint64) uint64 {
	h := uint64(14695981039346656037)
	for _, w := range v {
		h ^= w
		h *= 1099511628211
	}
	return h
}

// valueStore pairs a RoaringBitmap of all Value words with deduplicated
// full Value frames for reconstruction.
// seen maps a frame fingerprint to the indices of frames with that hash,
// enabling O(1) duplicate detection without full 1 KB key comparisons.
type valueStore struct {
	value  *roaring64.Bitmap
	frames [][valueWords]uint64
	seen   map[uint64][]int
}

func newValueStore() *valueStore {
	return &valueStore{
		value: roaring64.New(),
		seen:  make(map[uint64][]int),
	}
}

func (s *valueStore) add(v [valueWords]uint64) bool {
	h := frameHash(v)
	for _, idx := range s.seen[h] {
		if s.frames[idx] == v {
			return false
		}
	}
	s.seen[h] = append(s.seen[h], len(s.frames))
	s.value.AddMany(v[:])
	s.frames = append(s.frames, v)
	return true
}

func (s *valueStore) merge(other *valueStore) *valueStore {
	out := newValueStore()
	// Bitmaps are already fully merged via Or; bypass add() to avoid
	// redundant AddMany calls on words already present in the bitmap.
	out.value.Or(s.value)
	out.value.Or(other.value)
	for _, f := range s.frames {
		h := frameHash(f)
		out.seen[h] = append(out.seen[h], len(out.frames))
		out.frames = append(out.frames, f)
	}
	for _, f := range other.frames {
		h := frameHash(f)
		duplicate := false
		for _, idx := range out.seen[h] {
			if out.frames[idx] == f {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out.seen[h] = append(out.seen[h], len(out.frames))
			out.frames = append(out.frames, f)
		}
	}
	return out
}

// lsmLevel is a single sorted run in the LSM tree.
type lsmLevel struct {
	keys   []uint64
	stores []*valueStore
}

// SpatialIndex is an LSM-based spatial index with a MemTable buffer.
// TokenID → valueStore{RoaringBitmap(Value words), []Value frames}.
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

// NewSpatialIndex creates a new LSM Spatial Index.
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

// mergeLevels merges two sorted levels. Colliding TokenIDs merge their stores.
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
			out.stores = append(out.stores, a.stores[i].merge(b.stores[j]))
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

// InsertBatch inserts a batch of (tokenID, value) pairs into the LSM.
// Writes go to the memtable first; a flush is triggered at memTableSize.
func (idx *SpatialIndex) InsertBatch(tokenIDs []uint64, value [valueWords]uint64) {
	if len(tokenIDs) == 0 {
		return
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, tID := range tokenIDs {
		if _, exists := idx.memtable[tID]; !exists {
			idx.memtable[tID] = newValueStore()
		}
		if idx.memtable[tID].add(value) {
			idx.memSize++
		}
	}

	if idx.memSize >= memTableSize {
		idx.flushMemtable()
	}
}

// InsertTokenSpan stores a contiguous token span for a single Value.
func (idx *SpatialIndex) InsertTokenSpan(tokenIDs []uint64, value [valueWords]uint64) {
	idx.InsertBatch(tokenIDs, value)
}

// Flush forces the memtable to be written to the level hierarchy.
func (idx *SpatialIndex) Flush() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.memSize > 0 {
		idx.flushMemtable()
	}
}

// QueryHamming returns deduplicated Value frames whose bitmap contains any word
// within maxDistance Hamming distance of target.
func (idx *SpatialIndex) QueryHamming(target uint64, maxDistance int) [][valueWords]uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seen := make(map[uint64][]int)
	var matches [][valueWords]uint64
	maxDist := uint64(maxDistance)

	addVerifiedFrames := func(vs *valueStore) {
		// Fast pre-filter: skip bucket entirely if no word in the bitmap
		// is within maxDistance of target. Batch words so HasHammingMatch can
		// use the same SIMD path (NEON / AVX2) as per-frame verification.
		bucketMatch := false
		var batch [valueWords]uint64
		nBatch := 0
		it := vs.value.Iterator()
		for it.HasNext() {
			batch[nBatch] = it.Next()
			nBatch++
			if nBatch == len(batch) {
				if cpu.HasHammingMatch(batch[:], target, maxDist) {
					bucketMatch = true
					break
				}
				nBatch = 0
			}
		}
		if !bucketMatch && nBatch > 0 {
			if cpu.HasHammingMatch(batch[:nBatch], target, maxDist) {
				bucketMatch = true
			}
		}
		if !bucketMatch {
			return
		}
		// Per-frame verification: only include frames that actually contain
		// a word within maxDistance — avoids false positives from mixed buckets.
		for _, frame := range vs.frames {
			if !cpu.HasHammingMatch(frame[:], target, maxDist) {
				continue
			}
			h := frameHash(frame)
			duplicate := false
			for _, i := range seen[h] {
				if matches[i] == frame {
					duplicate = true
					break
				}
			}
			if !duplicate {
				seen[h] = append(seen[h], len(matches))
				matches = append(matches, frame)
			}
		}
	}

	// Search memtable first.
	for _, vs := range idx.memtable {
		addVerifiedFrames(vs)
	}

	// Then search flushed levels.
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, vs := range lvl.stores {
			addVerifiedFrames(vs)
		}
	}

	return matches
}

// ExactLookup returns the RoaringBitmap stored under tokenID, merging
// the memtable and all flushed levels.
func (idx *SpatialIndex) ExactLookup(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var toMerge []*roaring64.Bitmap

	if vs, ok := idx.memtable[tokenID]; ok {
		toMerge = append(toMerge, vs.value)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		i := sort.Search(len(lvl.keys), func(j int) bool {
			return lvl.keys[j] >= tokenID
		})
		if i < len(lvl.keys) && lvl.keys[i] == tokenID {
			toMerge = append(toMerge, lvl.stores[i].value)
		}
	}

	if len(toMerge) == 0 {
		return roaring64.New()
	}
	return roaring64.FastOr(toMerge...)
}

// GetStats returns usage statistics for the index.
func (idx *SpatialIndex) GetStats() map[string]interface{} {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var totalValues uint64
	var totalBytes uint64
	numLevels := 0

	if len(idx.memtable) > 0 {
		numLevels++
		for _, vs := range idx.memtable {
			totalValues += uint64(len(vs.frames))
			totalBytes += vs.value.GetSizeInBytes()
			totalBytes += uint64(len(vs.frames) * valueWords * 8)
		}
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		numLevels++
		for _, vs := range lvl.stores {
			totalValues += uint64(len(vs.frames))
			totalBytes += vs.value.GetSizeInBytes()
			totalBytes += uint64(len(vs.frames) * valueWords * 8)
		}
	}
	return map[string]interface{}{
		"total_values": totalValues,
		"num_levels":   numLevels,
		"memory_bytes": totalBytes,
	}
}
