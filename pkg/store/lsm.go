package store

import (
	"math/bits"
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
)

type tokenIDSorter struct {
	tokenIDs []uint64
	perm     []int
}

func normalizeTokenIDs(tokenIDs []uint64) []uint64 {
	if len(tokenIDs) == 0 {
		return nil
	}
	sort.Slice(tokenIDs, func(i, j int) bool { return tokenIDs[i] < tokenIDs[j] })
	out := tokenIDs[:1]
	for _, tokenID := range tokenIDs[1:] {
		if tokenID != out[len(out)-1] {
			out = append(out, tokenID)
		}
	}
	return out
}

func (s tokenIDSorter) Len() int           { return len(s.perm) }
func (s tokenIDSorter) Less(i, j int) bool { return s.tokenIDs[s.perm[i]] < s.tokenIDs[s.perm[j]] }
func (s tokenIDSorter) Swap(i, j int)      { s.perm[i], s.perm[j] = s.perm[j], s.perm[i] }

// lsmLevel is a single sorted run in the LSM tree.
// Keys are sorted TokenIDs; each maps to a Roaring Bitmap of Affinities.
type lsmLevel struct {
	keys    []uint64
	bitmaps []*roaring64.Bitmap
}

// SpatialIndex is an LSM-based spatial index.
// TokenID → RoaringBitmap(Affinities).
// Collision is compression: when two entries share the same TokenID,
// their affinity bitmaps are Or'd together.
type SpatialIndex struct {
	mu     sync.RWMutex
	levels []*lsmLevel
}

var (
	defaultSpatialIndexMu sync.RWMutex
	defaultSpatialIndex   = NewSpatialIndex()
)

// NewSpatialIndex creates a new LSM Spatial Index.
func NewSpatialIndex() *SpatialIndex {
	return &SpatialIndex{}
}

// DefaultSpatialIndex returns the shared repository-wide LSM used for
// token/affinity indexing during Value construction.
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
// It is mainly intended for deterministic tests.
func ResetDefaultSpatialIndex() *SpatialIndex {
	defaultSpatialIndexMu.Lock()
	defer defaultSpatialIndexMu.Unlock()
	defaultSpatialIndex = NewSpatialIndex()
	return defaultSpatialIndex
}

// InsertTokenSpan stores a contiguous token span under a single affinity word.
func (idx *SpatialIndex) InsertTokenSpan(tokenIDs []uint64, affinity uint64) {
	if idx == nil || len(tokenIDs) == 0 {
		return
	}

	affinities := make([]uint64, len(tokenIDs))

	for i := range affinities {
		affinities[i] = affinity
	}

	idx.InsertBatch(tokenIDs, affinities)
}

// mergeLevels merges two sorted levels. When TokenIDs collide,
// it Or's the affinity bitmaps.
func mergeLevels(a, b *lsmLevel) *lsmLevel {
	out := &lsmLevel{
		keys:    make([]uint64, 0, len(a.keys)+len(b.keys)),
		bitmaps: make([]*roaring64.Bitmap, 0, len(a.keys)+len(b.keys)),
	}

	i, j := 0, 0
	for i < len(a.keys) && j < len(b.keys) {
		ka, kb := a.keys[i], b.keys[j]
		switch {
		case ka < kb:
			out.keys = append(out.keys, ka)
			out.bitmaps = append(out.bitmaps, a.bitmaps[i])
			i++
		case kb < ka:
			out.keys = append(out.keys, kb)
			out.bitmaps = append(out.bitmaps, b.bitmaps[j])
			j++
		default: // Collision: same TokenID -> Or the affinity bitmaps
			a.bitmaps[i].Or(b.bitmaps[j])
			out.keys = append(out.keys, ka)
			out.bitmaps = append(out.bitmaps, a.bitmaps[i])
			i++
			j++
		}
	}
	for ; i < len(a.keys); i++ {
		out.keys = append(out.keys, a.keys[i])
		out.bitmaps = append(out.bitmaps, a.bitmaps[i])
	}
	for ; j < len(b.keys); j++ {
		out.keys = append(out.keys, b.keys[j])
		out.bitmaps = append(out.bitmaps, b.bitmaps[j])
	}
	return out
}

// InsertBatch inserts a batch of (tokenID, affinity) pairs into the LSM.
// Duplicate TokenIDs compress into a single Roaring Bitmap of Affinities.
func (idx *SpatialIndex) InsertBatch(tokenIDs []uint64, affinities []uint64) {
	n := len(tokenIDs)
	if n == 0 {
		return
	}

	// Sort by TokenID via an index permutation.
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	// Zero-allocation sort
	sort.Sort(tokenIDSorter{tokenIDs: tokenIDs, perm: perm})

	// Count distinct keys so we can pre-allocate the level exactly once.
	distinct := 1
	for i := 1; i < n; i++ {
		if tokenIDs[perm[i]] != tokenIDs[perm[i-1]] {
			distinct++
		}
	}

	lvl := &lsmLevel{
		keys:    make([]uint64, 0, distinct),
		bitmaps: make([]*roaring64.Bitmap, 0, distinct),
	}

	// Walk the sorted permutation, collecting contiguous runs per TokenID
	// and bulk-inserting the corresponding Affinities.
	buf := make([]uint64, 0, n)
	runStart := 0
	for runStart < n {
		curKey := tokenIDs[perm[runStart]]
		runEnd := runStart + 1
		for runEnd < n && tokenIDs[perm[runEnd]] == curKey {
			runEnd++
		}

		buf = buf[:0]
		for k := runStart; k < runEnd; k++ {
			buf = append(buf, affinities[perm[k]])
		}

		// Roaring ingests sorted values more efficiently.
		sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })

		bm := roaring64.New()
		bm.AddMany(buf)

		lvl.keys = append(lvl.keys, curKey)
		lvl.bitmaps = append(lvl.bitmaps, bm)

		runStart = runEnd
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// LSM cascade: merge into the first empty level slot.
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
}

// QueryHamming returns the TokenIDs whose affinity values are within the
// given Hamming distance of the target.
func (idx *SpatialIndex) QueryHamming(targetAffinity uint64, maxDistance int) []uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var matches []uint64

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for i := range lvl.keys {
			// Iterate through affinities in this TokenID's bitmap
			it := lvl.bitmaps[i].Iterator()
			for it.HasNext() {
				affinity := it.Next()
				if bits.OnesCount64(affinity^targetAffinity) <= maxDistance {
					matches = append(matches, lvl.keys[i])
					break
				}
			}
		}
	}

	return normalizeTokenIDs(matches)
}

// ReverseLookup returns the TokenIDs whose affinity bitmap contains the exact
// target affinity value. This is the exact reverse query of ExactLookup.
func (idx *SpatialIndex) ReverseLookup(targetAffinity uint64) []uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var matches []uint64
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for i, key := range lvl.keys {
			if lvl.bitmaps[i].Contains(targetAffinity) {
				matches = append(matches, key)
			}
		}
	}

	return normalizeTokenIDs(matches)
}

// ExactLookup returns the Roaring Bitmap of Affinities stored for a TokenID.
func (idx *SpatialIndex) ExactLookup(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var bitmaps []*roaring64.Bitmap
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		i := sort.Search(len(lvl.keys), func(j int) bool {
			return lvl.keys[j] >= tokenID
		})
		if i < len(lvl.keys) && lvl.keys[i] == tokenID {
			bitmaps = append(bitmaps, lvl.bitmaps[i])
		}
	}

	if len(bitmaps) == 0 {
		return roaring64.New()
	}
	if len(bitmaps) == 1 {
		return bitmaps[0].Clone()
	}

	// Use Roaring's optimized bulk OR for multiple bitmaps
	return roaring64.FastOr(bitmaps...)
}

// Intersect returns the Affinities shared by both TokenIDs.
func (idx *SpatialIndex) Intersect(tokenIDA, tokenIDB uint64) *roaring64.Bitmap {
	return roaring64.And(idx.ExactLookup(tokenIDA), idx.ExactLookup(tokenIDB))
}

// GetStats returns usage statistics for the index.
func (idx *SpatialIndex) GetStats() map[string]interface{} {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var totalAffinities uint64
	var totalBytes uint64
	numLevels := 0
	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		numLevels++
		for _, bm := range lvl.bitmaps {
			totalAffinities += bm.GetCardinality()
			totalBytes += bm.GetSizeInBytes()
		}
	}
	return map[string]interface{}{
		"total_affinities": totalAffinities,
		"num_levels":       numLevels,
		"memory_bytes":     totalBytes,
	}
}
