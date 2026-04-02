package store

import (
	"math"
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring/v2"
	bsi "github.com/RoaringBitmap/roaring/v2/BitSliceIndexing"
	"github.com/RoaringBitmap/roaring/v2/roaring64"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

const valueWords = 128

/*
FrameWords is the number of uint64 lanes in a Value frame (must match primitive.Words).
*/
const FrameWords = valueWords

/*
ValueFrameBitmap builds a Roaring bitmap of all set bits in the 128×64-bit Value
frame: bit i of the flat layout (word*64+bitIndex) is present iff it is set in v.
*/
func ValueFrameBitmap(v [valueWords]uint64) *roaring64.Bitmap {
	bitmap := roaring64.New()
	bitBase := 0
	for wordIndex := range valueWords {
		word := v[wordIndex]
		for bitIndex := range 64 {
			if word>>bitIndex&1 != 0 {
				bitmap.Add(uint64(bitBase + bitIndex))
			}
		}
		bitBase += 64
	}
	return bitmap
}

type valueStore struct {
	// postings records which ValueID(s) were indexed under this token key.
	// LSM compaction ORs postings; aggregate ExactLookup ORs ValueFrameBitmap(frame) for each id.
	postings *roaring64.Bitmap
}

func newValueStore() *valueStore {
	return &valueStore{postings: roaring64.New()}
}

func mergeStores(left, right *valueStore) *valueStore {
	out := newValueStore()
	out.postings.Or(left.postings)
	out.postings.Or(right.postings)
	return out
}

/*
lsmLevel is a single sorted run in the LSM tree.
*/
type lsmLevel struct {
	keys   []uint64
	stores []*valueStore
}

/*
SpatialIndex is a hybrid inverted index plus bit-sliced metadata columns.

Token keys map to Roaring sets of ValueIDs. Full-frame ExactLookup semantics are
preserved by OR-ing ValueFrameBitmap over every posted ValueID (frames are stored
once per ValueID). Roaring BitSliceIndexing BSIs hold PC, FW, sequence, and
accumulator, prev, and next using dense column ids, because github.com/RoaringBitmap BSI encodes
column keys as uint32 while ValueIDs are uint64 (including structure frames).
*/
type SpatialIndex struct {
	mu       sync.RWMutex
	memtable map[uint64]*valueStore
	memSize  int
	levels   []*lsmLevel

	frames map[uint64][valueWords]uint64

	// Dense BSI row handles: roaring BSI cannot use arbitrary uint64 ValueIDs as columns.
	valueToDense map[uint64]uint32
	denseToValue []uint64

	metaPC          *bsi.BSI
	metaFW          *bsi.BSI
	metaSequence    *bsi.BSI
	metaAccumulator *bsi.BSI
	metaPrev        *bsi.BSI
	metaNext        *bsi.BSI

	// postingTombstones lists ValueIDs logically removed from inverted-index reads until
	// ProcessPostingsTombstones peels them from physical posting bitmaps.
	postingTombstones *roaring64.Bitmap
}

var (
	defaultSpatialIndexMu sync.RWMutex
	defaultSpatialIndex   = NewSpatialIndex()
)

/*
NewSpatialIndex creates a new hybrid LSM index.
*/
func NewSpatialIndex() *SpatialIndex {
	return &SpatialIndex{
		memtable:          make(map[uint64]*valueStore),
		frames:            make(map[uint64][valueWords]uint64),
		valueToDense:      make(map[uint64]uint32),
		metaPC:            bsi.NewDefaultBSI(),
		metaFW:            bsi.NewDefaultBSI(),
		metaSequence:      bsi.NewDefaultBSI(),
		metaAccumulator:   bsi.NewDefaultBSI(),
		metaPrev:          bsi.NewDefaultBSI(),
		metaNext:          bsi.NewDefaultBSI(),
		postingTombstones: roaring64.New(),
	}
}

/*
DefaultSpatialIndex returns the shared repository-wide LSM.
*/
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

/*
ResetDefaultSpatialIndex swaps the shared index for a fresh instance.
*/
func ResetDefaultSpatialIndex() *SpatialIndex {
	defaultSpatialIndexMu.Lock()
	defer defaultSpatialIndexMu.Unlock()
	defaultSpatialIndex = NewSpatialIndex()
	return defaultSpatialIndex
}

/*
mergeLevels merges two sorted levels. Colliding TokenIDs merge their postings with OR.
*/
func mergeLevels(left, right *lsmLevel) *lsmLevel {
	out := &lsmLevel{
		keys:   make([]uint64, 0, len(left.keys)+len(right.keys)),
		stores: make([]*valueStore, 0, len(left.stores)+len(right.stores)),
	}

	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left.keys) && rightIndex < len(right.keys) {
		leftKey, rightKey := left.keys[leftIndex], right.keys[rightIndex]
		switch {
		case leftKey < rightKey:
			out.keys = append(out.keys, leftKey)
			out.stores = append(out.stores, left.stores[leftIndex])
			leftIndex++

		case rightKey < leftKey:
			out.keys = append(out.keys, rightKey)
			out.stores = append(out.stores, right.stores[rightIndex])
			rightIndex++

		default:
			out.keys = append(out.keys, leftKey)
			out.stores = append(out.stores, mergeStores(left.stores[leftIndex], right.stores[rightIndex]))
			leftIndex++
			rightIndex++
		}
	}

	for ; leftIndex < len(left.keys); leftIndex++ {
		out.keys = append(out.keys, left.keys[leftIndex])
		out.stores = append(out.stores, left.stores[leftIndex])
	}

	for ; rightIndex < len(right.keys); rightIndex++ {
		out.keys = append(out.keys, right.keys[rightIndex])
		out.stores = append(out.stores, right.stores[rightIndex])
	}

	return out
}

/*
flushMemtable converts the memtable map into a sorted lsmLevel and merges it
into the level hierarchy. Must be called with mu held.
*/
func (idx *SpatialIndex) flushMemtable() {
	keys := make([]uint64, 0, len(idx.memtable))
	for key := range idx.memtable {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	lvl := &lsmLevel{
		keys:   keys,
		stores: make([]*valueStore, len(keys)),
	}
	for storeIndex, key := range keys {
		lvl.stores[storeIndex] = idx.memtable[key]
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

/*
InsertBatch associates ValueIDs derived from value with the given token keys,
stores the frame once, and updates metadata BSIs. Same-token accumulation ORs
postings then ORs frame bitmaps on ExactLookup, matching the previous bit-union
semantics without destroying per-Value metadata.
*/
func (idx *SpatialIndex) InsertBatch(tokenIDs []uint64, value [valueWords]uint64) {
	if len(tokenIDs) == 0 {
		return
	}

	idWord := core.Cfg.Value.Region.ID.Start
	rowID := value[idWord]

	idx.mu.Lock()
	defer idx.mu.Unlock()

	var frameCopy [valueWords]uint64
	copy(frameCopy[:], value[:])
	idx.frames[rowID] = frameCopy

	if idx.postingTombstones != nil {
		idx.postingTombstones.Remove(rowID)
	}

	dense := idx.denseFor(rowID)
	reg := core.Cfg.Value.Region
	idx.metaPC.SetValue(uint64(dense), int64(value[reg.Registers.PC]))
	idx.metaFW.SetValue(uint64(dense), int64(value[reg.Registers.FW]))
	idx.metaSequence.SetValue(uint64(dense), int64(value[reg.State.Sequence]))
	idx.metaAccumulator.SetValue(uint64(dense), int64(value[reg.State.Accumulator]))
	idx.metaPrev.SetValue(uint64(dense), int64(value[reg.Prev.Start]))
	idx.metaNext.SetValue(uint64(dense), int64(value[reg.Next.Start]))

	for _, tokenID := range tokenIDs {
		if _, exists := idx.memtable[tokenID]; !exists {
			idx.memtable[tokenID] = newValueStore()
		}
		idx.memtable[tokenID].postings.Add(rowID)
		idx.memSize++
	}

	if idx.memSize >= core.Cfg.System.QueueSize {
		idx.flushMemtable()
	}
}

/*
InsertTokenSpan stores a contiguous token span for a single Value frame.
*/
func (idx *SpatialIndex) InsertTokenSpan(tokenIDs []uint64, value [valueWords]uint64) {
	idx.InsertBatch(tokenIDs, value)
}

/*
Flush forces the memtable to be written to the level hierarchy.
*/
func (idx *SpatialIndex) Flush() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.memSize > 0 || len(idx.memtable) > 0 {
		idx.flushMemtable()
	}
}

func (idx *SpatialIndex) mergedPostingsLocked(tokenID uint64) *roaring64.Bitmap {
	var parts []*roaring64.Bitmap

	if store, ok := idx.memtable[tokenID]; ok {
		parts = append(parts, store.postings)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		position := sort.Search(len(lvl.keys), func(j int) bool {
			return lvl.keys[j] >= tokenID
		})
		if position < len(lvl.keys) && lvl.keys[position] == tokenID {
			parts = append(parts, lvl.stores[position].postings)
		}
	}

	if len(parts) == 0 {
		return roaring64.New()
	}

	merged := roaring64.FastOr(parts...)
	if idx.postingTombstones != nil && !idx.postingTombstones.IsEmpty() {
		merged.AndNot(idx.postingTombstones)
	}
	return merged
}

func (idx *SpatialIndex) aggregateFrameBitmapLocked(postings *roaring64.Bitmap) *roaring64.Bitmap {
	out := roaring64.New()
	iter := postings.Iterator()
	for iter.HasNext() {
		valueID := iter.Next()
		frame, ok := idx.frames[valueID]
		if !ok {
			continue
		}
		bitmap := ValueFrameBitmap(frame)
		out.Or(bitmap)
	}
	return out
}

func (idx *SpatialIndex) exactLookupLocked(tokenID uint64) *roaring64.Bitmap {
	postings := idx.mergedPostingsLocked(tokenID)
	return idx.aggregateFrameBitmapLocked(postings)
}

/*
ExactLookup returns the Roaring bitmap stored under tokenID (memtable and levels merged):
OR of ValueFrameBitmap for every posted ValueID.
*/
func (idx *SpatialIndex) ExactLookup(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.exactLookupLocked(tokenID)
}

/*
LookupKeysByValue returns every tokenID whose stored bitmap equals the encoding
of v. This scans all keys in the index (memtable and flushed levels).
*/
func (idx *SpatialIndex) LookupKeysByValue(value *primitive.Value) []uint64 {
	target := ValueFrameBitmap([128]uint64(*value))

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seen := make(map[uint64]struct{})
	var out []uint64
	try := func(tokenID uint64) {
		if _, ok := seen[tokenID]; ok {
			return
		}
		got := idx.exactLookupLocked(tokenID)
		if got.Equals(target) {
			seen[tokenID] = struct{}{}
			out = append(out, tokenID)
		}
	}

	for tokenID := range idx.memtable {
		try(tokenID)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		for _, tokenID := range lvl.keys {
			try(tokenID)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

/*
LookupKeysByValueID returns every tokenID whose merged postings include valueID.

This tracks the ingest-time association even when the live frame no longer
matches ValueFrameBitmap equality (for example after firmware mutates words
between tokenizer Insert and prompt Read).
*/
func (idx *SpatialIndex) LookupKeysByValueID(valueID uint64) []uint64 {
	if valueID == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	seen := make(map[uint64]struct{})
	var out []uint64

	try := func(tokenID uint64) {
		if _, ok := seen[tokenID]; ok {
			return
		}

		postings := idx.mergedPostingsLocked(tokenID)
		if postings.Contains(valueID) {
			seen[tokenID] = struct{}{}
			out = append(out, tokenID)
		}
	}

	for tokenID := range idx.memtable {
		try(tokenID)
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}

		for _, tokenID := range lvl.keys {
			try(tokenID)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

/*
ValueIDsForToken returns a clone of merged ValueID postings for tokenID.
*/
func (idx *SpatialIndex) ValueIDsForToken(tokenID uint64) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.mergedPostingsLocked(tokenID).Clone()
}

/*
FrameByValueID returns a stored frame copy when present.
*/
func (idx *SpatialIndex) FrameByValueID(valueID uint64) ([valueWords]uint64, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	frame, ok := idx.frames[valueID]
	return frame, ok
}

func (idx *SpatialIndex) denseFor(valueID uint64) uint32 {
	if dense, ok := idx.valueToDense[valueID]; ok {
		return dense
	}

	if len(idx.denseToValue) > math.MaxUint32 {
		panic("store.SpatialIndex: dense BSI column count exceeds math.MaxUint32")
	}

	dense := uint32(len(idx.denseToValue))
	idx.valueToDense[valueID] = dense
	idx.denseToValue = append(idx.denseToValue, valueID)
	return dense
}

func (idx *SpatialIndex) denseMaskFromValueIDsLocked(valueIDs *roaring64.Bitmap) *roaring.Bitmap {
	mask := roaring.New()
	if valueIDs == nil || valueIDs.IsEmpty() {
		return mask
	}
	iter := valueIDs.Iterator()
	for iter.HasNext() {
		id := iter.Next()
		if dense, ok := idx.valueToDense[id]; ok {
			mask.Add(dense)
		}
	}
	return mask
}

func (idx *SpatialIndex) valueIDsFromDenseLocked(dense *roaring.Bitmap) *roaring64.Bitmap {
	out := roaring64.New()
	if dense == nil || dense.IsEmpty() {
		return out
	}
	iter := dense.Iterator()
	for iter.HasNext() {
		column := iter.Next()
		if int(column) < len(idx.denseToValue) {
			valueID := idx.denseToValue[column]
			if valueID != 0 {
				out.Add(valueID)
			}
		}
	}
	return out
}

func (idx *SpatialIndex) compareRegister(
	parallelism int,
	column *bsi.BSI,
	op bsi.Operation,
	needle, end int64,
	tokenValueIDs *roaring64.Bitmap,
) *roaring64.Bitmap {
	var candidates *roaring.Bitmap
	if tokenValueIDs == nil {
		candidates = column.GetExistenceBitmap().Clone()
	} else {
		if tokenValueIDs.IsEmpty() {
			return roaring64.New()
		}
		candidates = idx.denseMaskFromValueIDsLocked(tokenValueIDs)
	}

	if candidates.IsEmpty() {
		return roaring64.New()
	}

	denseResult := column.CompareValue(parallelism, op, needle, end, candidates)
	return idx.valueIDsFromDenseLocked(denseResult)
}

/*
ComparePC filters ValueIDs by program counter using BSI. tokenValueIDs constrains
the universe (e.g. inverted index output); nil scans all indexed rows.
*/
func (idx *SpatialIndex) ComparePC(parallelism int, op bsi.Operation, needle, end int64, tokenValueIDs *roaring64.Bitmap) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaPC, op, needle, end, tokenValueIDs)
}

/*
CompareFW filters ValueIDs by firmware register word (see core.FirmwareRegister*).
*/
func (idx *SpatialIndex) CompareFW(parallelism int, op bsi.Operation, needle, end int64, tokenValueIDs *roaring64.Bitmap) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaFW, op, needle, end, tokenValueIDs)
}

/*
CompareSequence filters ValueIDs by state.sequence.
*/
func (idx *SpatialIndex) CompareSequence(parallelism int, op bsi.Operation, needle, end int64, tokenValueIDs *roaring64.Bitmap) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaSequence, op, needle, end, tokenValueIDs)
}

/*
CompareAccumulator filters ValueIDs by state.accumulator.
*/
func (idx *SpatialIndex) CompareAccumulator(parallelism int, op bsi.Operation, needle, end int64, tokenValueIDs *roaring64.Bitmap) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaAccumulator, op, needle, end, tokenValueIDs)
}

/*
ComparePrev filters ValueIDs by Prev pointer register value.
*/
func (idx *SpatialIndex) ComparePrev(
	parallelism int,
	op bsi.Operation,
	needle, end int64,
	tokenValueIDs *roaring64.Bitmap,
) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaPrev, op, needle, end, tokenValueIDs)
}

/*
CompareNext filters ValueIDs by Next pointer register value.
*/
func (idx *SpatialIndex) CompareNext(
	parallelism int,
	op bsi.Operation,
	needle, end int64,
	tokenValueIDs *roaring64.Bitmap,
) *roaring64.Bitmap {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.compareRegister(parallelism, idx.metaNext, op, needle, end, tokenValueIDs)
}

/*
GetStats returns usage statistics for the index.
*/
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
			totalBytes += vs.postings.GetSizeInBytes()
		}
	}

	for _, lvl := range idx.levels {
		if lvl == nil {
			continue
		}
		numLevels++
		for _, vs := range lvl.stores {
			totalKeys++
			totalBytes += vs.postings.GetSizeInBytes()
		}
	}

	totalBytes += uint64(len(idx.frames)) * valueWords * 8
	totalBytes += uint64(len(idx.valueToDense)) * 16

	return map[string]interface{}{
		"total_keys":      totalKeys,
		"num_levels":      numLevels,
		"memory_bytes":    totalBytes,
		"frames":          len(idx.frames),
		"bsi_rows":        len(idx.denseToValue),
		"meta_pc_bits":    idx.metaPC.BitCount(),
		"meta_fw_bits":    idx.metaFW.BitCount(),
		"meta_seq_bits":   idx.metaSequence.BitCount(),
		"meta_accum_bits": idx.metaAccumulator.BitCount(),
		"meta_prev_bits":  idx.metaPrev.BitCount(),
		"meta_next_bits":  idx.metaNext.BitCount(),
	}
}
