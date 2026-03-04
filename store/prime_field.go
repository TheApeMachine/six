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
*/
type PrimeField struct {
	mu     sync.RWMutex
	chords []data.MultiChord
	N      int

	// buf keeps the last 21 chords to compute MultiChords dynamically on insert
	buf []data.Chord
}

func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:      0,
		chords: make([]data.MultiChord, 0),
		buf:    make([]data.Chord, 0, 21),
	}
}

/*
Insert appends a chord to the flat field and records its Morton key
for decode. It automatically expands the single chord into a MultiChord
across all Fibonacci Windows. Returns the index assigned.
*/
func (field *PrimeField) Insert(chord data.Chord) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.buf = append(field.buf, chord)
	
	if len(field.buf) > 21 {
		field.buf = field.buf[1:]
	}

	var multi data.MultiChord
	fibs := []int{3, 5, 8, 13, 21}

	for i, w := range fibs {
		start := max(len(field.buf)-w, 0)

		var agg data.Chord
		for _, c := range field.buf[start:] {
			for j := range agg {
				agg[j] |= c[j]
			}
		}
		multi[i] = agg
	}

	field.chords = append(field.chords, multi)
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
Chord returns the raw MultiChord at a given index.
*/
func (field *PrimeField) MultiChord(idx int) data.MultiChord {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.chords[idx]
}

/*
Mask temporarily zeros out a chord to exclude it from BestFill searches.
It returns the original chord so it can be unmasked later.
*/
func (field *PrimeField) Mask(idx int) data.MultiChord {
	field.mu.Lock()
	defer field.mu.Unlock()

	original := field.chords[idx]
	field.chords[idx] = data.MultiChord{}
	return original
}

/*
Unmask restores a previously masked chord.
*/
func (field *PrimeField) Unmask(idx int, chord data.MultiChord) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.chords[idx] = chord
}
