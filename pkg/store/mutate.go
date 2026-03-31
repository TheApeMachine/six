package store

import (
	"github.com/RoaringBitmap/roaring/v2"
)

/*
RemoveValueID drops a stored frame, clears BSI metadata for that ValueID, and removes
the id from every token posting list (full index scan). Use when tombstoning a Value
so Compare* and materialization no longer see it.

Dense column slots are left as holes (denseToValue[slot]=0); re-inserting the same
ValueID allocates a fresh dense column.
*/
func (idx *SpatialIndex) RemoveValueID(valueID uint64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.frames, valueID)

	if dense, ok := idx.valueToDense[valueID]; ok {
		mask := roaring.New()
		mask.Add(dense)
		idx.metaPC.ClearValues(mask)
		idx.metaFW.ClearValues(mask)
		idx.metaSequence.ClearValues(mask)
		idx.metaAccumulator.ClearValues(mask)
		delete(idx.valueToDense, valueID)
		if int(dense) < len(idx.denseToValue) {
			idx.denseToValue[dense] = 0
		}
	}

	for _, vs := range idx.memtable {
		vs.postings.Remove(valueID)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, vs := range lvl.stores {
			vs.postings.Remove(valueID)
		}
	}
}
