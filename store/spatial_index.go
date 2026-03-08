package store

import (
	"sort"
	"sync"

	"github.com/theapemachine/six/data"
)

/*
LSMSpatialIndex is the geometric domain's data structure.
Collision is Compression.
Stores Chord values directly — no encode/decode round-trip.

Keys use the existing Morton encoding from tokenizer.MortonCoder:

	Layout: [24 zero bits | 8 bits Symbol | 32 bits Position]
	- Bits 32-55: symbol identity
	- Bits 0-31:  absolute replay position
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
NewLSMSpatialIndex allocates an empty LSM index. cellSize is reserved for future use.
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

/*
Count returns the number of unique key-value entries across all LSM levels.
*/
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

	for level, keys := range idx.levelsKeys {
		if keys == nil {
			continue
		}
		// Binary search within this level
		lo, hi := 0, len(keys)-1
		for lo <= hi {
			mid := (lo + hi) / 2
			if keys[mid] == tokenID {
				return idx.levelsVals[level][mid]
			} else if keys[mid] < tokenID {
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
	}

	return data.Chord{}
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
position.

O(log N) per LSM level via binary search.
*/
func (idx *LSMSpatialIndex) QueryByByte(b byte) []data.Chord {
	lo := uint64(b) << 32
	hi := lo | 0xFFFFFFFF
	return idx.QueryRange(lo, hi)
}

/*
QueryNeighborhood returns all chords that share the same byte value
and fall within posRadius positions of the given position.

This is the BestFill pre-filter: given a prompt token's Morton key,
narrow the store down to the local neighborhood for that byte identity.

Key layout: [24 zero bits | 8 bits Symbol | 32 bits Position]
So "same byte, nearby position" = keys in
[byte<<32 | (pos-r), byte<<32 | (pos+r)].
*/
func (idx *LSMSpatialIndex) QueryNeighborhood(key uint64, posRadius uint32) []data.Chord {
	symbol := (key >> 32) & 0xFF
	pos := uint32(key)

	lo := uint32(0)
	if pos > posRadius {
		lo = pos - posRadius
	}

	hi := pos + posRadius
	if hi < pos {
		hi = ^uint32(0)
	}

	base := symbol << 32
	return idx.QueryRange(
		base|uint64(lo),
		base|uint64(hi),
	)
}
