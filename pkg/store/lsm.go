package store

import (
	"math/bits"
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
)

const (
	valueWords   = 128
	memTableSize = 1000
)

// valueStore pairs a RoaringBitmap of all Value words with deduplicated
// full Value frames for reconstruction.
type valueStore struct {
	value  *roaring64.Bitmap
	frames [][valueWords]uint64
	seen   map[[valueWords]uint64]struct{}
}

func newValueStore() *valueStore {
	return &valueStore{
		value: roaring64.New(),
		seen:  make(map[[valueWords]uint64]struct{}),
	}
}

func (s *valueStore) add(v [valueWords]uint64) {
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.value.AddMany(v[:])
	s.frames = append(s.frames, v)
}

func (s *valueStore) merge(other *valueStore) *valueStore {
	out := newValueStore()
	out.value.Or(s.value)
	out.value.Or(other.value)
	for _, f := range s.frames {
		out.add(f)
	}
	for _, f := range other.frames {
		out.add(f)
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
		idx.memtable[tID].add(value)
		idx.memSize++
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

	seen := make(map[[valueWords]uint64]struct{})
	var matches [][valueWords]uint64

	// Search memtable first.
	for _, vs := range idx.memtable {
		it := vs.value.Iterator()
		for it.HasNext() {
			if bits.OnesCount64(it.Next()^target) <= maxDistance {
				for _, frame := range vs.frames {
					if _, ok := seen[frame]; !ok {
						seen[frame] = struct{}{}
						matches = append(matches, frame)
					}
				}
				break
			}
		}
	}

	// Then search flushed levels.
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, vs := range lvl.stores {
			it := vs.value.Iterator()
			for it.HasNext() {
				if bits.OnesCount64(it.Next()^target) <= maxDistance {
					for _, frame := range vs.frames {
						if _, ok := seen[frame]; !ok {
							seen[frame] = struct{}{}
							matches = append(matches, frame)
						}
					}
					break
				}
			}
		}
	}

	return matches
}

// ExactLookup returns the RoaringBitmap stored under tokenID, merging
// the memtable and all flushed levels.
func (idx *SpatialIndex) ExactLookup(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := roaring64.New()

	if vs, ok := idx.memtable[tokenID]; ok {
		result.Or(vs.value)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		i := sort.Search(len(lvl.keys), func(j int) bool {
			return lvl.keys[j] >= tokenID
		})
		if i < len(lvl.keys) && lvl.keys[i] == tokenID {
			result.Or(lvl.stores[i].value)
		}
	}
	return result
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
