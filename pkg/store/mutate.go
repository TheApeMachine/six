package store

import (
	"github.com/RoaringBitmap/roaring/v2"
	"github.com/RoaringBitmap/roaring/v2/roaring64"
)

/*
RemoveValueID drops the stored frame and BSI row and adds valueID to postingTombstones.
Inverted-index reads (ValueIDsForToken, ExactLookup) subtract tombstones via mergedPostingsLocked,
so postings lists are consistent logically without an O(tokens) scan.

Physical posting bitmaps still contain valueID until ProcessPostingsTombstones runs; call it
periodically or after batches so memory and iteration over raw stores stay accurate.
Reuse the same ValueID in InsertBatch clears its tombstone automatically.
*/
func (idx *SpatialIndex) RemoveValueID(valueID uint64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.postingTombstones == nil {
		idx.postingTombstones = roaring64.New()
	}
	idx.postingTombstones.Add(valueID)

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
}

/*
ProcessPostingsTombstones applies postingTombstones to every valueStore in the memtable and
all LSM levels via AndNot, then clears postingTombstones. Hold idx.mu for the whole operation;
cost is O(total token keys) bitmap operations plus Roaring work proportional to tombstone set size.

Call from a batch worker or after bursts of RemoveValueID to reclaim dense posting storage.
*/
func (idx *SpatialIndex) ProcessPostingsTombstones() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.postingTombstones == nil || idx.postingTombstones.IsEmpty() {
		return
	}

	pending := idx.postingTombstones.Clone()
	idx.postingTombstones.Clear()

	for _, vs := range idx.memtable {
		vs.postings.AndNot(pending)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, vs := range lvl.stores {
			vs.postings.AndNot(pending)
		}
	}
}

/*
RemoveValueIDImmediate removes valueID from every posting list synchronously (O(total distinct
token keys) stores). Use when a single removal must be visible on disk-shaped structures that
ignore postingTombstones. Also clears the id from postingTombstones if present.
*/
func (idx *SpatialIndex) RemoveValueIDImmediate(valueID uint64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.postingTombstones != nil {
		idx.postingTombstones.Remove(valueID)
	}

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
