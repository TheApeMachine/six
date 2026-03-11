package data

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/pool"
)

/*
Sanitize zeroes bits [257..511] to enforce the 257-bit logical width invariant.
Bit 256 (the delimiter face) is preserved. Word 4 keeps its lowest bit
(bit 256); words 5..7 are fully zeroed.
*/
func (chord *Chord) Sanitize() {
	// Word layout: word[0] = bits 0..63, word[1] = 64..127, ...
	// word[4] = bits 256..319 → only bit 256 (the LSB) is valid.
	chord.SetC4(chord.C4() & 1) // keep only bit 256
	chord.SetC5(0)
	chord.SetC6(0)
	chord.SetC7(0)
}

func (chord *Chord) block(i int) uint64 {
	switch i {
	case 0:
		return chord.C0()
	case 1:
		return chord.C1()
	case 2:
		return chord.C2()
	case 3:
		return chord.C3()
	case 4:
		return chord.C4()
	case 5:
		return chord.C5()
	case 6:
		return chord.C6()
	case 7:
		return chord.C7()
	default:
		return 0
	}
}

func (chord *Chord) setBlock(i int, v uint64) {
	switch i {
	case 0:
		chord.SetC0(v)
	case 1:
		chord.SetC1(v)
	case 2:
		chord.SetC2(v)
	case 3:
		chord.SetC3(v)
	case 4:
		chord.SetC4(v)
	case 5:
		chord.SetC5(v)
	case 6:
		chord.SetC6(v)
	case 7:
		chord.SetC7(v)
	}
}

/*
Has checks if the prime at index p is active in the chord.
*/
func (chord *Chord) Has(p int) bool {
	return (chord.block(p/64) & (1 << (p % 64))) != 0
}

/*
Set activates the prime at index p.
*/
func (chord *Chord) Set(p int) {
	chord.setBlock(p/64, chord.block(p/64)|(1<<(p%64)))
}

/*
Clear deactivates the prime at index p.
*/
func (chord *Chord) Clear(p int) {
	chord.setBlock(p/64, chord.block(p/64)&^(1<<(p%64)))
}

/*
Byte finds the byte whose BaseChord matches this chord's logical signature.
Zeros word[7] before lookup so sequence metadata does not affect the match.
*/
func (chord *Chord) Byte() byte {
	var search Chord = *chord
	search.SetC7(0) // Strip sequence pointer

	for b := range 256 {
		test := BaseChord(byte(b))

		if search == test {
			return byte(b)
		}
	}

	return 0
}

/*
BestByte decodes the chord to the nearest lexical byte.
It first attempts exact BaseChord recovery. If that fails, it falls back to the
intrinsic face with strongest overlap.
*/
func (chord *Chord) BestByte() byte {
	if exact := chord.Byte(); exact != 0 || *chord == BaseChord(0) {
		return exact
	}

	face := chord.IntrinsicFace()
	if face >= 0 && face < 256 {
		return byte(face)
	}

	return 0
}

/*
RotationSeed derives a structural affine seed from the chord itself.
Unlike a popcount-only mapping, this uses the actual active prime layout so
distinct chords with identical density can still drive different rotations.
*/
func (chord *Chord) RotationSeed() (uint16, uint16) {
	if chord.ActiveCount() == 0 {
		return 1, 0
	}

	var accA uint32 = 1
	var accB uint32

	for blockIdx := range config.ChordBlocks {
		block := chord.block(blockIdx)
		if block == 0 {
			continue
		}

		mix := uint32(block^(block>>29)^(block>>43)) & 0x1FFFF
		accA = (accA*131 + mix + uint32(blockIdx+1)*17) % 257
		accB = (accB*137 + mix + uint32(Popcount(block))*29 + uint32(blockIdx+1)*31) % 257

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx >= 257 {
				block &= block - 1
				continue
			}

			prime := uint32(primeIdx + 1)
			accA = (accA + prime*prime + prime*23 + uint32(bitIdx+1)*7) % 257
			accB = (accB + prime*67 + uint32(bitIdx+1)*13) % 257

			block &= block - 1
		}
	}

	if accA == 0 {
		accA = 1
	}

	return uint16(accA), uint16(accB % 257)
}

/*
Bytes returns the chord as config.ChordBlocks×8 bytes (big-endian uint64s).
*/
func (chord *Chord) Bytes() []byte {
	b := make([]byte, config.ChordBlocks*8)
	for i := range config.ChordBlocks {
		binary.BigEndian.PutUint64(b[i*8:], chord.block(i))
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
MaskChord returns a control-plane marker used to denote an unresolved gap or
masked region in a sequence without colliding with any lexical BaseChord.
*/
func MaskChord() Chord {
	var chord Chord
	chord.Set(256)

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
		c.setBlock(i, binary.BigEndian.Uint64(b[i*8:]))
	}

	return c
}

/*
ChordLCM returns the element-wise OR of chords — the LCM in prime exponent space.
Used for aggregating span chords (words, sentences, n-grams).
*/
func ChordLCM(chords []Chord) (lcm Chord) {
	for _, ch := range chords {
		chord := &ch
		lcm.setBlock(0, lcm.block(0)|chord.block(0))
		lcm.setBlock(1, lcm.block(1)|chord.block(1))
		lcm.setBlock(2, lcm.block(2)|chord.block(2))
		lcm.setBlock(3, lcm.block(3)|chord.block(3))
		lcm.setBlock(4, lcm.block(4)|chord.block(4))
		lcm.setBlock(5, lcm.block(5)|chord.block(5))
		lcm.setBlock(6, lcm.block(6)|chord.block(6))
		lcm.setBlock(7, lcm.block(7)|chord.block(7))
	}

	lcm.Sanitize()
	return lcm
}

/*
ActiveCount returns the number of active basis primes in this
chord using popcount.
*/
func (chord Chord) ActiveCount() (n int) {
	return Popcount(chord.C0()) + Popcount(chord.C1()) + Popcount(chord.C2()) + Popcount(chord.C3()) +
		Popcount(chord.C4()) + Popcount(chord.C5()) + Popcount(chord.C6()) + Popcount(chord.C7())
}

/*
popcount counts the number of 1-bits in a uint64
*/
func Popcount(x uint64) (count int) {
	return bits.OnesCount64(x)
}

/*
ChordPrimeIndices returns the prime indices (0..NBasis-1) that are set in the chord.
Used for debug output (which primes were assigned).
*/
func ChordPrimeIndices(chord *Chord) []int {
	var out []int

	for i := range config.ChordBlocks {
		block := chord.block(i)

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
AND returns the element-wise AND of two chords (their GCD in
prime exponent space). Shared factors.
*/
func (chord *Chord) AND(other Chord) Chord {
	gcd, _ := NewChord(chord.Segment())
	gcd.setBlock(0, chord.block(0)&other.block(0))
	gcd.setBlock(1, chord.block(1)&other.block(1))
	gcd.setBlock(2, chord.block(2)&other.block(2))
	gcd.setBlock(3, chord.block(3)&other.block(3))
	gcd.setBlock(4, chord.block(4)&other.block(4))
	gcd.setBlock(5, chord.block(5)&other.block(5))
	gcd.setBlock(6, chord.block(6)&other.block(6))
	gcd.setBlock(7, chord.block(7)&other.block(7))
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
		block := c.block(i)
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
	return Popcount(a.C0()&b.C0()) + Popcount(a.C1()&b.C1()) + Popcount(a.C2()&b.C2()) + Popcount(a.C3()&b.C3()) +
		Popcount(a.C4()&b.C4()) + Popcount(a.C5()&b.C5()) + Popcount(a.C6()&b.C6()) + Popcount(a.C7()&b.C7())
}

/*
ChordHole returns target AND NOT existing — bits set in target but not in existing.
*/
func ChordHole(target, existing *Chord) (hole Chord) {
	hole.setBlock(0, target.block(0)&^existing.block(0))
	hole.setBlock(1, target.block(1)&^existing.block(1))
	hole.setBlock(2, target.block(2)&^existing.block(2))
	hole.setBlock(3, target.block(3)&^existing.block(3))
	hole.setBlock(4, target.block(4)&^existing.block(4))
	hole.setBlock(5, target.block(5)&^existing.block(5))
	hole.setBlock(6, target.block(6)&^existing.block(6))
	hole.setBlock(7, target.block(7)&^existing.block(7))
	return hole
}

/*
OR returns the element-wise OR of two chords (their LCM in prime exponent space).
*/
func (chord *Chord) OR(other Chord) Chord {
	lcm, _ := NewChord(chord.Segment())
	lcm.setBlock(0, chord.block(0)|other.block(0))
	lcm.setBlock(1, chord.block(1)|other.block(1))
	lcm.setBlock(2, chord.block(2)|other.block(2))
	lcm.setBlock(3, chord.block(3)|other.block(3))
	lcm.setBlock(4, chord.block(4)|other.block(4))
	lcm.setBlock(5, chord.block(5)|other.block(5))
	lcm.setBlock(6, chord.block(6)|other.block(6))
	lcm.setBlock(7, chord.block(7)|other.block(7))
	lcm.Sanitize()
	return lcm
}

/*
XOR returns the element-wise XOR of two chords (for cancellative superposition).
*/
func (chord Chord) XOR(other Chord) Chord {
	xor, _ := NewChord(chord.Segment())
	xor.setBlock(0, chord.block(0)^other.block(0))
	xor.setBlock(1, chord.block(1)^other.block(1))
	xor.setBlock(2, chord.block(2)^other.block(2))
	xor.setBlock(3, chord.block(3)^other.block(3))
	xor.setBlock(4, chord.block(4)^other.block(4))
	xor.setBlock(5, chord.block(5)^other.block(5))
	xor.setBlock(6, chord.block(6)^other.block(6))
	xor.setBlock(7, chord.block(7)^other.block(7))
	xor.Sanitize()
	return xor
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
		block := chord.block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			flat.ActivePrimes[flat.Count] = uint16(i*64) + bitIdx
			flat.Count++
			block &= block - 1
		}
	}

	return flat
}

/*
FlattenBatched converts a slice of sparse Chords into a slice of FlatChords.
If a pool is provided, each chord is scheduled as an independent task and the
pool's built-in scaler handles concurrency — no manual worker-count tuning.
Falls back to synchronous execution when no pool is available.
*/
func FlattenBatched(chords []Chord, p *pool.Pool) []FlatChord {
	n := len(chords)
	out := make([]FlatChord, n)

	if n == 0 {
		return out
	}

	wg := sync.WaitGroup{}

	for i := range chords {
		wg.Add(1)
		idx := i

		p.Schedule(fmt.Sprintf("flatten-%d", idx), func() (any, error) {
			defer wg.Done()
			out[idx] = chords[idx].Flatten()
			return nil, nil
		})
	}

	wg.Wait()
	return out
}

/*
IntrinsicFace returns the byte face (0-255) whose BaseChord has maximum overlap
with this chord. Returns 256 if no face has at least 2 matching primes.
*/
func (chord *Chord) IntrinsicFace() int {
	if chord.ActiveCount() == 0 {
		return 256
	}

	bestFace := 256
	bestSim := 0

	for b := range 256 {
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
RollLeft circular-shifts the chord within the 257-bit logical width.
Binds sequential position to geometry before superposition.
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
		block := chord.block(i)
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

/*
BindPosition preserves the intrinsic chord identity while adding a positional
orbit copy.
*/
func (chord *Chord) BindPosition(pos int) Chord {
	shifted := chord.RollLeft(pos)

	return chord.OR(shifted)
}

/*
BindGeometry preserves the base chord, adds positional binding, and then
superposes an optional carrier chord.
*/
func (chord *Chord) BindGeometry(pos int, carrier *Chord) Chord {
	bound := chord.BindPosition(pos)

	if carrier == nil || carrier.ActiveCount() == 0 {
		return bound
	}

	return bound.OR(*carrier)
}
