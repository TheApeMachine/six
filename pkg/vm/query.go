package vm

import (
	"github.com/RoaringBitmap/roaring/v2/roaring64"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

/*
ResolvePromptIntersection returns ValueIDs that appear under every token key
Tokenize(prompt[i], i) — the same keys NewValue uses when indexing each byte.
This is the substrate analogue of “query cancels against ingested rows that
share this exact prefix at these positions” (README query narrative).

Empty prompt or nil index yields nil. If any position has no postings, the
intersection is empty.
*/
func ResolvePromptIntersection(idx *store.SpatialIndex, prompt []byte) []uint64 {
	if idx == nil || len(prompt) == 0 {
		return nil
	}

	parts := make([]*roaring64.Bitmap, 0, len(prompt))
	for index, b := range prompt {
		tid := primitive.Tokenize(b, uint64(index))
		bmp := idx.ValueIDsForToken(tid)
		parts = append(parts, bmp)
	}

	merged := store.AndValueIDs(parts...)

	return store.ValueIDsToSlice(merged)
}

/*
PrevChainBackward walks stored frames from startValueID following Prev
(region id word) until Prev is zero or maxSteps is reached. The first element
is startValueID; each subsequent element is the parent linked from the previous
frame. Useful for testing signal-cut → canonical ancestry.
*/
func PrevChainBackward(idx *store.SpatialIndex, startValueID uint64, maxSteps int) []uint64 {
	if idx == nil || startValueID == 0 || maxSteps <= 0 {
		return nil
	}

	prevWord := core.Cfg.Value.Region.Prev.Start
	var chain []uint64

	cur := startValueID

	for step := 0; step < maxSteps && cur != 0; step++ {
		chain = append(chain, cur)
		frame, ok := idx.FrameByValueID(cur)
		if !ok {
			break
		}

		prevID := frame[prevWord]
		if prevID == 0 {
			break
		}

		cur = prevID
	}

	return chain
}
