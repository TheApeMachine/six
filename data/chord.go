package data

import (
	"encoding/binary"
	"math/bits"
	"sync"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/pool"
)

/*
Chord is the prime signature bitset. Storage is [8]uint64 (512 bits) for GPU
alignment, but only the lower 257 bits are logically valid — one bit per face
of the Fermat cube (CubeFaces = 257). Bits [257..511] must always be zero.
Call Sanitize() after any raw bitwise OR to enforce this invariant.
*/
type Chord [config.ChordBlocks]uint64

/*
Sanitize zeroes bits [257..511] to enforce the 257-bit logical width invariant.
Bit 256 (the delimiter face) is preserved. Word 4 keeps its lowest bit
(bit 256); words 5..7 are fully zeroed.
*/
func (chord *Chord) Sanitize() {
	// Word layout: word[0] = bits 0..63, word[1] = 64..127, ...
	// word[4] = bits 256..319 → only bit 256 (the LSB) is valid.
	chord[4] &= 1 // keep only bit 256
	chord[5] = 0
	chord[6] = 0
	chord[7] = 0
}

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
Bytes returns the chord as config.ChordBlocks×8 bytes (big-endian uint64s).
*/
func (chord *Chord) Bytes() []byte {
	b := make([]byte, config.ChordBlocks*8)
	for i := range config.ChordBlocks {
		binary.BigEndian.PutUint64(b[i*8:], chord[i])
	}
	return b
}

/*
BaseChord returns a deterministic base chord for a byte value.
Uses coprime spreading to set 5 bits in the 257-bit logical chord space,
ensuring each of the 256 byte values gets a unique signature.
*/
func BaseChord(b byte) Chord {
	var chord Chord
	const logicalBits = 257 // CubeFaces — must match geometry.CubeFaces

	// 5 coprime multipliers spread across the logical chord space
	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		bit := off % logicalBits
		chord.Set(bit)
	}

	return chord
}

/*
ChordFromBytes parses ChordBlocks×8 bytes (big-endian) back into a Chord.
*/
func ChordFromBytes(b []byte) (c Chord) {
	if len(b) < config.ChordBlocks*8 {
		return c
	}

	for i := range config.ChordBlocks {
		c[i] = binary.BigEndian.Uint64(b[i*8:])
	}

	return c
}

/*
ChordToByte compares the logical signature of the chord against the 256 possible bytes.
It strips the sequence pointer in word[7] before checking.
*/
func ChordToByte(chord *Chord) byte {
	var search Chord = *chord
	search[7] = 0 // Strip sequence pointer
	for b := 0; b < 256; b++ {
		test := BaseChord(byte(b))
		if search == test {
			return byte(b)
		}
	}
	return 0
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

	lcm.Sanitize()
	return lcm
}

/*
ActiveCount returns the number of active basis primes in this
chord using popcount.
*/
func (chord *Chord) ActiveCount() (n int) {
	for i := range config.ChordBlocks {
		n += popcount(chord[i])
	}
	return n
}

/*
popcount counts the number of 1-bits in a uint64
*/
func popcount(x uint64) (count int) {
	return bits.OnesCount64(x)
}

/*
ChordPrimeIndices returns the prime indices (0..NBasis-1) that are set in the chord.
Used for debug output (which primes were assigned).
*/
func ChordPrimeIndices(chord *Chord) []int {
	var out []int

	for i := range config.ChordBlocks {
		block := chord[i]

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < config.NBasis {
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
	for i := range config.ChordBlocks {
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
	seeds := [8]uint64{
		0x9e3779b185ebca87,
		0xc2b2ae3d27d4eb4f,
		0x165667b19e3779f9,
		0x85ebca77c2b2ae63,
		0x27d4eb2f165667c5,
		0x94d049bb133111eb,
		0xd6e8feb86659fd93,
		0xa4093822299f31d1,
	}

	var acc [8]int

	for i := range config.ChordBlocks {
		block := c[i]
		for block != 0 {
			bit := bits.TrailingZeros64(block)
			idx := uint64(i*64 + bit + 1)

			for j := range seeds {
				h := idx*seeds[j] + (idx << uint(j+1))
				if h>>63 == 1 {
					acc[j]++
				} else {
					acc[j]--
				}
			}

			block &= block - 1
		}
	}

	var bin int
	for j := range acc {
		if acc[j] >= 0 {
			bin |= 1 << j
		}
	}

	return bin
}

/*
ChordSimilarity returns the number of shared prime exponents (popcount of AND).
*/
func ChordSimilarity(a, b *Chord) (sim int) {
	for i := range config.ChordBlocks {
		sim += popcount(a[i] & b[i])
	}

	return sim
}

/*
ChordHole computes the "structural vacuum" — what's missing from a
target chord given the parts we already have (target AND NOT existing).
*/
func ChordHole(target, existing *Chord) (hole Chord) {
	for i := range config.ChordBlocks {
		hole[i] = target[i] &^ existing[i]
	}

	return hole
}

/*
ChordOR returns the element-wise OR of two chords (their LCM in prime exponent space).
*/
func ChordOR(a, b *Chord) (lcm Chord) {
	for i := range config.ChordBlocks {
		lcm[i] = a[i] | b[i]
	}

	lcm.Sanitize()
	return lcm
}

/*
FlatChord is a dense array of active prime indices used for optimal GPU iteration.
It eliminates bit-twiddling thread divergence in SIMT architectures.
*/
type FlatChord struct {
	ActivePrimes [config.NBasis]uint16
	Count        uint16
	_            uint16 // Padding
}

/*
Flatten converts the sparse bitset into a densely packed array of active prime indices.
*/
func (chord *Chord) Flatten() FlatChord {
	var flat FlatChord

	for i := range config.ChordBlocks {
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

var (
	batchPool       *pool.Pool
	flattenPoolOnce sync.Once
)

func initFlattenPool() {
	batchPool = pool.NewPool()
}

/*
FlattenBatched converts a slice of sparse Chords into a slice of FlatChords asychronously.
It uses a pre-warmed, auto-scaling worker pool to prevent CPU-bound loop starvation
without the overhead of goroutine creation.
*/
func FlattenBatched(chords []Chord, workers int) []FlatChord {
	flattenPoolOnce.Do(initFlattenPool)

	if workers <= 0 {
		workers = 4
	}
	n := len(chords)
	out := make([]FlatChord, n)

	if n == 0 {
		return out
	}

	chunkSize := (n + workers - 1) / workers
	if chunkSize == 0 {
		chunkSize = 1
	}

	done := make(chan struct{}, workers)

	activeWorkers := 0
	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := min(start+chunkSize, n)
		if start >= n {
			break
		}

		s, e := start, end
		batchPool.Do(func() {
			for i := s; i < e; i++ {
				out[i] = chords[i].Flatten()
			}
			done <- struct{}{}
		})
		activeWorkers++
	}

	for i := 0; i < activeWorkers; i++ {
		<-done
	}

	return out
}

/*
IntrinsicFace returns a deterministic face index (0-255) for a chord based
on its lowest active prime within the structural range. Returns 256 if none.
*/
func (chord *Chord) IntrinsicFace() int {
	if chord.ActiveCount() == 0 {
		return 256
	}

	bestFace := 256
	bestSim := 0

	for b := 0; b < 256; b++ {
		bc := BaseChord(byte(b))
		sim := ChordSimilarity(chord, &bc)
		if sim > bestSim {
			bestSim = sim
			bestFace = b
		}
	}

	// Because BaseChords are 5 bits dense, we require at least 2 matching
	// prime factors to assume deliberate resonance over random noise overlap.
	if bestSim < 2 {
		return 256
	}

	return bestFace
}

/*
RollLeft executes a discrete spatial permutation (circular shift) on the chord.
This permanently binds sequential position to the semantic geometry before superposition.
*/
func (chord *Chord) RollLeft(shift int) Chord {
	if shift == 0 {
		return *chord
	}

	const logicalBits = 257 // CubeFaces
	var out Chord
	shift = shift % logicalBits

	// Fast sparse-array permutation within the 257-bit logical width
	for i := range config.ChordBlocks {
		block := chord[i]
		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < logicalBits {
				newPrimeIdx := (primeIdx + shift) % logicalBits
				out.Set(newPrimeIdx)
			}

			block &= block - 1
		}
	}

	return out
}
