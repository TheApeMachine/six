/*
Package store implements SpatialIndex: a hybrid LSM-style inverted index (token → ValueIDs)
with Roaring BitSliceIndexing columns for PC, firmware, sequence, and accumulator, plus
full Value frame retention for ExactLookup and SIMD-friendly materialization.

Typical consumption:

 1. Insert: idx.InsertBatch(tokenKeys, frame) — same as before; frame[cfg.Value.Region.ID.Start] is the row key.

 2. Relational-style narrowing: candidates := idx.ValueIDsForToken(tokenX); subset :=
    store.AndValueIDs(idx.ComparePC(0, bsi.LT, 100, 0, candidates), idx.CompareFW(0, bsi.EQ, int64(fw), 0, candidates))

 3. Vector / VSA path: ids := store.ValueIDsToSlice(subset); buf := idx.MaterializeTokenRegionWords(ids); pospop.Count64(&counts, buf)

 4. Tombstone: idx.RemoveValueID(valueID) hides the row from index reads immediately; call
    idx.ProcessPostingsTombstones() in batches to peel IDs off physical posting bitmaps (O(token keys)).
    idx.RemoveValueIDImmediate(valueID) does synchronous posting removal when you cannot rely on the tombstone set.

BSI column ids are dense uint32s internal to the index; compare helpers always return *roaring64.Bitmap of ValueIDs.
*/
package store
