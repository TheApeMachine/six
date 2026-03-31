package store

import (
	"github.com/RoaringBitmap/roaring/v2/roaring64"

	pospop "github.com/theapemachine/six/pkg/compute/kernel/cpu/csa"
	"github.com/theapemachine/six/pkg/core"
)

/*
TokenRegionWordCount returns how many uint64 words fall under value.region.tokens
(length of the contiguous token slice used for holographic token data).
*/
func TokenRegionWordCount() int {
	bits := core.Cfg.Value.Region.Tokens.Bits
	if bits == 0 {
		return 0
	}
	return int((bits + 63) / 64)
}

/*
MaterializeTokenRegionWords returns a new buffer holding token-region words in row-major
order: for each id in valueIDs (in order), TokenWordCount() uint64s are copied from the
stored frame. Missing frames are zero-filled so len(result) == len(valueIDs)*TokenRegionWordCount().
*/
func (idx *SpatialIndex) MaterializeTokenRegionWords(valueIDs []uint64) []uint64 {
	wordCount := TokenRegionWordCount()
	if wordCount <= 0 || len(valueIDs) == 0 {
		return nil
	}

	out := make([]uint64, len(valueIDs)*wordCount)
	idx.MaterializeTokenRegionWordsInto(valueIDs, out)
	return out
}

/*
MaterializeTokenRegionWordsInto copies token-region words into dst. dst must have length
at least len(valueIDs)*TokenRegionWordCount(). Returns the number of uint64 elements written.
*/
func (idx *SpatialIndex) MaterializeTokenRegionWordsInto(valueIDs []uint64, dst []uint64) int {
	wordCount := TokenRegionWordCount()
	if wordCount <= 0 || len(valueIDs) == 0 {
		return 0
	}

	need := len(valueIDs) * wordCount
	if len(dst) < need {
		return 0
	}

	tokenStart := core.Cfg.Value.Region.Tokens.Start
	if tokenStart < 0 || tokenStart+wordCount > FrameWords {
		return 0
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	offset := 0
	for _, valueID := range valueIDs {
		frame, ok := idx.frames[valueID]
		if !ok {
			offset += wordCount
			continue
		}
		copy(dst[offset:offset+wordCount], frame[tokenStart:tokenStart+wordCount])
		offset += wordCount
	}

	return need
}

/*
TokenRegionPositionalPopcount runs SIMD positional population count (csa / pospop) over the
concatenated token-region words of the given ValueIDs. See github.com/theapemachine/six/pkg/compute/kernel/cpu/csa Count64.
*/
func (idx *SpatialIndex) TokenRegionPositionalPopcount(valueIDs []uint64) [64]int {
	var counts [64]int
	buf := idx.MaterializeTokenRegionWords(valueIDs)
	if len(buf) == 0 {
		return counts
	}

	pospop.Count64(&counts, buf)
	return counts
}

/*
AndValueIDs returns the set intersection of all non-nil bitmaps. Empty or nil list yields an empty bitmap.
*/
func AndValueIDs(parts ...*roaring64.Bitmap) *roaring64.Bitmap {
	out := roaring64.New()
	if len(parts) == 0 {
		return out
	}

	first := true
	for _, part := range parts {
		if part == nil || part.IsEmpty() {
			return roaring64.New()
		}
		if first {
			out.Or(part.Clone())
			first = false
			continue
		}
		out.And(part)
	}
	return out
}

/*
ValueIDsToSlice copies bitmap contents into a sorted slice (iterator order).
*/
func ValueIDsToSlice(bitmap *roaring64.Bitmap) []uint64 {
	if bitmap == nil || bitmap.IsEmpty() {
		return nil
	}
	out := make([]uint64, 0, bitmap.GetCardinality())
	iter := bitmap.Iterator()
	for iter.HasNext() {
		out = append(out, iter.Next())
	}
	return out
}
