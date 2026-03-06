package store

import (
	"sort"
	"sync"

	"github.com/theapemachine/six/data"
)

/*
LSMSpatialIndex is the geometric domain's data structure.
Collision is Compression. High Z items exist solely in local memory.
Stores Chord values directly — no encode/decode round-trip.

Keys use the existing Morton encoding from tokenizer.MortonCoder:

	Layout: (byte_value << 24) | position
	- Bits 24-31: byte value (0-255)
	- Bits 0-23:  sequence position
*/
type LSMSpatialIndex struct {
	mu sync.RWMutex

	// LSM Levels: level 0 = new inserts, level N = merged inserts
	// keys and vals are sorted parallel arrays
	levelsKeys [][]uint64
	levelsVals [][]data.Chord

	totalCount int

	// Reverse index: chord → key
	reverse map[data.Chord]uint64
}

/*
NewLSMSpatialIndex creates a new LSMSpatialIndex.
*/
func NewLSMSpatialIndex(cellSize float64) *LSMSpatialIndex {
	return &LSMSpatialIndex{
		levelsKeys: make([][]uint64, 0),
		levelsVals: make([][]data.Chord, 0),
		reverse:    make(map[data.Chord]uint64),
	}
}

/*
Insert adds a new geometric token. If it matches an existing identity, it compresses.
key is the Morton-encoded token identity (EncodeTokenID or equivalent).
*/
func (idx *LSMSpatialIndex) Insert(key uint64, value data.Chord) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.insertSorted([]uint64{key}, []data.Chord{value})
	idx.reverse[value] = key
}

func (idx *LSMSpatialIndex) insertSorted(newKeys []uint64, newVals []data.Chord) {
	level := 0

	for level < len(idx.levelsKeys) {
		lvlKeys := idx.levelsKeys[level]
		lvlVals := idx.levelsVals[level]

		if lvlKeys == nil {
			break
		}

		mergedKeys, mergedVals := idx.mergeAndDeduplicate(
			lvlKeys, lvlVals,
			newKeys, newVals,
		)

		newKeys = mergedKeys
		newVals = mergedVals

		idx.levelsKeys[level] = nil
		idx.levelsVals[level] = nil
		level++
	}

	if level == len(idx.levelsKeys) {
		idx.levelsKeys = append(idx.levelsKeys, newKeys)
		idx.levelsVals = append(idx.levelsVals, newVals)
	} else {
		idx.levelsKeys[level] = newKeys
		idx.levelsVals[level] = newVals
	}

	idx.totalCount = 0

	for _, k := range idx.levelsKeys {
		idx.totalCount += len(k)
	}
}

/*
mergeAndDeduplicate merges two implicitly sorted key/val pairs.
*/
func (idx *LSMSpatialIndex) mergeAndDeduplicate(
	keysA []uint64, valsA []data.Chord,
	keysB []uint64, valsB []data.Chord,
) ([]uint64, []data.Chord) {
	sizeA := len(keysA)
	sizeB := len(keysB)

	outKeys := make([]uint64, 0, sizeA+sizeB)
	outVals := make([]data.Chord, 0, sizeA+sizeB)

	i, j := 0, 0
	for i < sizeA && j < sizeB {
		ka, kb := keysA[i], keysB[j]
		va, vb := valsA[i], valsB[j]

		if ka < kb {
			outKeys = append(outKeys, ka)
			outVals = append(outVals, va)
			i++
		} else if kb < ka {
			outKeys = append(outKeys, kb)
			outVals = append(outVals, vb)
			j++
		} else { // Collision
			if va == vb { // Identical token: Compress into one
				outKeys = append(outKeys, ka)
				outVals = append(outVals, va)
				i++
				j++
			} else {
				outKeys = append(outKeys, ka)
				outVals = append(outVals, va)
				i++
			}
		}
	}

	for i < sizeA {
		outKeys = append(outKeys, keysA[i])
		outVals = append(outVals, valsA[i])
		i++
	}

	for j < sizeB {
		outKeys = append(outKeys, keysB[j])
		outVals = append(outVals, valsB[j])
		j++
	}

	return outKeys, outVals
}

// Count returns the number of unique entries stored across all LSM levels.
func (idx *LSMSpatialIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalCount
}

/*
Lookup performs key → chord lookup. Binary search across all LSM levels.
Returns the chord and true if found, zero chord and false otherwise.
*/
func (idx *LSMSpatialIndex) Lookup(tokenID uint64) data.Chord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for _, keys := range idx.levelsKeys {
		if keys == nil {
			continue
		}
		// Binary search within this level
		lo, hi := 0, len(keys)-1
		for lo <= hi {
			mid := (lo + hi) / 2
			if keys[mid] == tokenID {
				return idx.levelsVals[indexOf(idx.levelsKeys, keys)][mid]
			} else if keys[mid] < tokenID {
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
	}

	return data.Chord{}
}

// indexOf returns the level index for a given keys slice.
func indexOf(levels [][]uint64, target []uint64) int {
	for i, l := range levels {
		if len(l) > 0 && len(target) > 0 && &l[0] == &target[0] {
			return i
		}
	}
	return 0
}

/*
ReverseLookup performs chord → key lookup from the reverse index.
*/
func (idx *LSMSpatialIndex) ReverseLookup(chord data.Chord) uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	key, ok := idx.reverse[chord]

	if !ok {
		return 0
	}

	return key
}

/*
QueryRange returns all chords whose Morton key falls within [lo, hi].
Binary search across all LSM levels. This is the primitive that
QueryByByte and QueryNeighborhood are built on.
*/
func (idx *LSMSpatialIndex) QueryRange(lo, hi uint64) []data.Chord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var results []data.Chord

	for lvl, keys := range idx.levelsKeys {
		if keys == nil {
			continue
		}

		// Binary search for the first key >= lo
		start := sort.Search(len(keys), func(j int) bool {
			return keys[j] >= lo
		})

		// Scan forward while key <= hi
		for i := start; i < len(keys) && keys[i] <= hi; i++ {
			results = append(results, idx.levelsVals[lvl][i])
		}
	}

	return results
}

/*
QueryByByte returns all chords for a given byte value, regardless of
position. Since keys are (byte << 24) | pos, all entries for byte b
live in the contiguous range [b<<24, (b+1)<<24 - 1].

O(log N) per LSM level via binary search.
*/
func (idx *LSMSpatialIndex) QueryByByte(b byte) []data.Chord {
	lo := uint64(b) << 24
	hi := lo | 0xFFFFFF
	return idx.QueryRange(lo, hi)
}

/*
QueryNeighborhood returns all chords that share the same byte value
and fall within posRadius positions of the given position.

This is the BestFill pre-filter: given a prompt token's Morton key,
narrow the 50M-entry store down to the spatial neighborhood before
dispatching to the GPU.

Key layout: (byte << 24) | pos
So "same byte, nearby position" = keys in [byte<<24 | (pos-r), byte<<24 | (pos+r)].
*/
func (idx *LSMSpatialIndex) QueryNeighborhood(key uint64, posRadius uint32) []data.Chord {
	byteVal := key >> 24
	pos := key & 0xFFFFFF

	lo := pos
	if uint32(pos) > posRadius {
		lo = pos - uint64(posRadius)
	} else {
		lo = 0
	}

	hi := pos + uint64(posRadius)
	if hi > 0xFFFFFF {
		hi = 0xFFFFFF
	}

	return idx.QueryRange(
		(byteVal<<24)|lo,
		(byteVal<<24)|hi,
	)
}
