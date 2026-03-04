package data

import (
	"encoding/binary"
	"math/bits"

	"github.com/theapemachine/six/numeric"
)

/*
Chord is the prime signature bitset. Size derived from numeric.NBasis via
numeric.ChordBlocks (NBasis/64). Change NBasis → Chord size updates everywhere.
*/
type Chord [numeric.ChordBlocks]uint64

/*
MultiChord represents a token's resonance across all 5 Fibonacci window scales.
Allows the GPU to compute multiscale consensus in a single parallel fetch.
*/
type MultiChord [5]Chord

/*
Has checks if the prime at index p is active in the chord.
*/
func (chord *Chord) Has(p int) bool {
	return (chord[p/64] & (1 << (p % 64))) != 0
}

/*
Set activates the prime at index p.
*/
func (chord *Chord) Set(p int) {
	chord[p/64] |= (1 << (p % 64))
}

/*
Clear deactivates the prime at index p.
*/
func (chord *Chord) Clear(p int) {
	chord[p/64] &^= (1 << (p % 64))
}

/*
Bytes returns the chord as numeric.ChordBlocks×8 bytes (big-endian uint64s).
*/
func (chord *Chord) Bytes() []byte {
	b := make([]byte, numeric.ChordBlocks*8)
	for i := range numeric.ChordBlocks {
		binary.BigEndian.PutUint64(b[i*8:], chord[i])
	}
	return b
}

/*
ChordFromBytes parses ChordBlocks×8 bytes (big-endian) back into a Chord.
*/
func ChordFromBytes(b []byte) (c Chord) {
	if len(b) < numeric.ChordBlocks*8 {
		return c
	}
	for i := 0; i < numeric.ChordBlocks; i++ {
		c[i] = binary.BigEndian.Uint64(b[i*8:])
	}
	return c
}

/*
ChordLCM returns the element-wise OR of chords — the LCM in prime exponent space.
Used for aggregating span chords (words, sentences, n-grams).
*/
func ChordLCM(chords []Chord) (lcm Chord) {
	for _, ch := range chords {
		for i := range lcm {
			lcm[i] |= ch[i]
		}
	}
	return lcm
}

/*
ActiveCount returns the number of active basis primes in this
chord using popcount.
*/
func (chord *Chord) ActiveCount() (n int) {
	for i := range numeric.ChordBlocks {
		n += popcount(chord[i])
	}
	return n
}

/*
popcount counts the number of 1-bits in a uint64
*/
func popcount(x uint64) (count int) {
	for x != 0 {
		x &= x - 1
		count++
	}

	return count
}

/*
ChordPrimeIndices returns the prime indices (0..NBasis-1) that are set in the chord.
Used for debug output (which primes were assigned).
*/
func ChordPrimeIndices(chord *Chord) []int {
	var out []int

	for i := range numeric.ChordBlocks {
		block := chord[i]

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < numeric.NBasis {
				out = append(out, primeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
ChordGCD returns the element-wise AND of two chords (their GCD in
prime exponent space). Shared factors.
*/
func ChordGCD(a, b *Chord) (gcd Chord) {
	for i := range numeric.ChordBlocks {
		gcd[i] = a[i] & b[i]
	}

	return gcd
}

/*
ChordBin maps a chord to a structural bin 0..255 for indexing phase tables.
Deterministic XOR-fold of the chord bits ensures similar chords map to nearby bins.
Enables chord-native co-occurrence and phase lookup without byte symbols.
*/
func ChordBin(c *Chord) int {
	var h uint64
	for i := range numeric.ChordBlocks {
		h ^= c[i]
	}
	return int(h % 256)
}

/*
ChordSimilarity returns the number of shared prime exponents (popcount of AND).
*/
func ChordSimilarity(a, b *Chord) (sim int) {
	for i := range numeric.ChordBlocks {
		sim += popcount(a[i] & b[i])
	}

	return sim
}

/*
ChordHole computes the "structural vacuum" — what's missing from a
target chord given the parts we already have (target AND NOT existing).
*/
func ChordHole(target, existing *Chord) (hole Chord) {
	for i := range numeric.ChordBlocks {
		hole[i] = target[i] &^ existing[i]
	}

	return hole
}



/*
ChordOR returns the element-wise OR of two chords (their LCM in prime exponent space).
*/
func ChordOR(a, b *Chord) (lcm Chord) {
	for i := range numeric.ChordBlocks {
		lcm[i] = a[i] | b[i]
	}
	return lcm
}

/*
FlatChord is a dense array of active prime indices used for optimal GPU iteration.
It eliminates bit-twiddling thread divergence in SIMT architectures.
*/
type FlatChord struct {
	ActivePrimes [numeric.NBasis]uint16
	Count        uint16
	_            uint16 // Padding
}

/*
Flatten converts the sparse bitset into a densely packed array of active prime indices.
*/
func (chord *Chord) Flatten() FlatChord {
	var flat FlatChord

	for i := range numeric.ChordBlocks {
		block := chord[i]
		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			flat.ActivePrimes[flat.Count] = uint16(i*64) + bitIdx
			flat.Count++
			block &= block - 1
		}
	}

	return flat
}
