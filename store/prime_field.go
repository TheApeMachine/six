package store

import (
	"sync"
	"unsafe"

	"github.com/theapemachine/six/data"
)

/*
PrimeField is the flat, contiguous chord array for GPU dispatch.

The LSM is cold storage (Morton key → chord → bytes). The PrimeField
is the compute-side representation: a dense 1D array of 512-bit chords
that the GPU scans in parallel via bitwise_best_fill.

Architecture:
  Ingest:  tokenizer → chord → LSM.Insert (storage) + PrimeField.Register (compute)
  Query:   context chord → GPU scans PrimeField → returns winning index
  Decode:  winning index → PrimeField.Key(idx) → LSM.Lookup → actual bytes

Each chord is 64 bytes (8 × uint64). 1M chords = 64MB — fits on any GPU.
*/
type PrimeField struct {
	mu     sync.RWMutex
	chords []data.Chord
	keys   []uint64
	N      int
}

func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:      0,
		chords: make([]data.Chord, 0),
		keys:   make([]uint64, 0),
	}
}

/*
Insert appends a chord to the flat field and records its Morton key
for decode. Returns the index assigned to this chord.
*/
func (field *PrimeField) Insert(chord data.Chord, key uint64) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.chords = append(field.chords, chord)
	field.keys = append(field.keys, key)
	field.N++
}

/*
Field returns a pointer to the contiguous chord array for GPU dispatch.
The caller must hold the data stable for the duration of the GPU call.
*/
func (field *PrimeField) Field() unsafe.Pointer {
	field.mu.RLock()
	defer field.mu.RUnlock()

	if field.N == 0 {
		return nil
	}

	return unsafe.Pointer(&field.chords[0])
}

/*
Chord returns the chord at a given index.
*/
func (field *PrimeField) Chord(idx int) data.Chord {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.chords[idx]
}

/*
Key returns the Morton key for a given index.
*/
func (field *PrimeField) Key(idx int) uint64 {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.keys[idx]
}

/*
Mask temporarily zeros out a chord to exclude it from BestFill searches.
It returns the original chord so it can be unmasked later.
*/
func (field *PrimeField) Mask(idx int) data.Chord {
	field.mu.Lock()
	defer field.mu.Unlock()

	original := field.chords[idx]
	field.chords[idx] = data.Chord{}
	return original
}

/*
Unmask restores a previously masked chord.
*/
func (field *PrimeField) Unmask(idx int, chord data.Chord) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.chords[idx] = chord
}
