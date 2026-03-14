package data

import (
	"context"
	"fmt"
	"math/bits"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/system/console"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

func BuildChord(payload []byte) (Chord, error) {
	console.Trace("Building chord", "payload", string(payload))
	logicalBits := config.Numeric.VocabSize + 1

	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return Chord{}, err
	}

	chord, err := NewRootChord(seg)
	if err != nil {
		return Chord{}, err
	}

	for pos, b := range payload {
		_, bSeg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		if err != nil {
			return Chord{}, err
		}

		byteChord, err := NewRootChord(bSeg)
		if err != nil {
			return Chord{}, err
		}

		// Pairwise coprime spreading factors (7,13,31,61,127) distribute byte influence across logicalBits.
		for _, off := range [5]int{
			int(b) * 7, int(b) * 13, int(b) * 31, int(b) * 61, int(b) * 127,
		} {
			byteChord.Set(off % logicalBits)
		}

		positioned := byteChord.RollLeft(pos)
		chord = chord.OR(positioned)
	}

	return chord, nil
}

/*
CopyFrom copies all 8 words from src into the receiver.
Replaces the repeated SetC0..SetC7 call pattern at every call site.
*/
func (chord *Chord) CopyFrom(src Chord) {
	for i := range 8 {
		chord.setBlock(i, src.block(i))
	}
}

/*
ChordSliceToList packs a Go slice of Chords into a Cap'n Proto Chord_List.
*/
func ChordSliceToList(chords []Chord) (Chord_List, error) {
	_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))

	if err != nil {
		return Chord_List{}, err
	}

	list, err := NewChord_List(seg, int32(len(chords)))

	if err != nil {
		return Chord_List{}, err
	}

	for i, cc := range chords {
		dst := list.At(i)
		dst.CopyFrom(cc)
	}

	return list, nil
}

/*
ChordListToSlice copies each entry into a freshly allocated Chord and returns the slice.
*/
func ChordListToSlice(list Chord_List) ([]Chord, error) {
	out := make([]Chord, list.Len())

	for i := 0; i < list.Len(); i++ {
		src := list.At(i)

		_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))

		if err != nil {
			return nil, err
		}

		chord, err := NewChord(seg)

		if err != nil {
			return nil, err
		}

		chord.CopyFrom(src)
		out[i] = chord
	}

	return out, nil
}

/*
Sanitize enforces the lower 257-bit field width for fundamental field comparisons,
but preserves the upper 255 bits (Guard Band) which is now utilized for 
Cross-Modal Alignment, Rotational Opcodes, and Residual Phase Carry.
*/
func (chord *Chord) Sanitize() {
	chord.SetC4(chord.C4() & 1) // Bit 256 is the delimiter
	// Words 5, 6, and 7 are deliberately kept alive as the Guard Band for Opcodes
	// See SetOpcode and SetResidualCarry.
}

/*
SetOpcode stores a navigational or grammatical opcode in the Guard Band (bits 320-383, Word 5).
*/
func (chord *Chord) SetOpcode(opcode uint64) {
	chord.SetC5(opcode)
}

/*
Opcode retrieves the opcode embedded in the Guard Band.
*/
func (chord *Chord) Opcode() uint64 {
	return chord.C5()
}

/*
SetResidualCarry stores fractional phase state across distributed wavefront computations (Word 6).
*/
func (chord *Chord) SetResidualCarry(carry uint64) {
	chord.SetC6(carry)
}

/*
ResidualCarry retrieves fractional phase context stored in the Guard Band.
*/
func (chord *Chord) ResidualCarry() uint64 {
	return chord.C6()
}

func (chord *Chord) Block(i int) uint64 {
	return chord.block(i)
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
BaseChord returns a deterministic 5-bit chord for a byte value.
Coprime spreading places exactly 5 bits in the 257-bit logical chord space,
giving C(257,5) = 8.8 billion unique signatures.
*/
func BaseChord(b byte) Chord {
	chord := MustNewChord()

	logicalBits := config.Numeric.VocabSize + 1

	// Pairwise coprime spreading factors (7,13,31,61,127) distribute byte influence across logicalBits.
	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		chord.Set(off % logicalBits)
	}

	return chord
}

/*
ShannonDensity returns the fraction of the 257 logical bits that are active.
The Sequencer uses this to force a boundary before the chord saturates.
Above ~0.40 (103 bits) the chord loses discriminative power.
*/
func (chord Chord) ShannonDensity() float64 {
	return float64(chord.ActiveCount()) / 257.0
}

/*
MaskChord returns a control-plane marker used to denote an unresolved gap or
masked region in a sequence without colliding with any lexical BaseChord.
*/
func MaskChord() Chord {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(fmt.Errorf("MaskChord allocation failed: %w", err))
	}
	chord, err := NewChord(seg)
	if err != nil {
		panic(fmt.Errorf("MaskChord allocation failed: %w", err))
	}
	chord.Set(config.Numeric.VocabSize)

	return chord
}

/*
ChordLCM returns the element-wise OR of chords — the LCM in prime exponent space.
Used for aggregating span chords (words, sentences, n-grams).
*/
func ChordLCM(chords []Chord) (lcm Chord) {
	lcm = MustNewChord()

	var c0, c1, c2, c3, c4 uint64

	for _, ch := range chords {
		c0 |= ch.C0()
		c1 |= ch.C1()
		c2 |= ch.C2()
		c3 |= ch.C3()
		c4 |= ch.C4()
	}

	lcm.SetC0(c0)
	lcm.SetC1(c1)
	lcm.SetC2(c2)
	lcm.SetC3(c3)
	lcm.SetC4(c4 & 1)
	lcm.SetC5(0)
	lcm.SetC6(0)
	lcm.SetC7(0)

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
ANDErr returns the element-wise AND of two chords (their GCD in
prime exponent space), checking allocation errors. Shared factors.
*/
func (chord *Chord) ANDErr(other Chord) (Chord, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return Chord{}, err
	}
	gcd, err := NewChord(seg)
	if err != nil {
		return Chord{}, err
	}
	gcd.setBlock(0, chord.block(0)&other.block(0))
	gcd.setBlock(1, chord.block(1)&other.block(1))
	gcd.setBlock(2, chord.block(2)&other.block(2))
	gcd.setBlock(3, chord.block(3)&other.block(3))
	gcd.setBlock(4, chord.block(4)&other.block(4))
	gcd.setBlock(5, chord.block(5)&other.block(5))
	gcd.setBlock(6, chord.block(6)&other.block(6))
	gcd.setBlock(7, chord.block(7)&other.block(7))
	return gcd, nil
}

/*
AND returns the element-wise AND of two chords (their GCD in
prime exponent space). Shared factors.
*/
func (chord *Chord) AND(other Chord) Chord {
	gcd, err := chord.ANDErr(other)
	if err != nil {
		panic(err)
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
func ChordHole(target, existing *Chord) Chord {
	hole := MustNewChord()
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
	lcm := MustNewChord()

	lcm.SetC0(chord.C0() | other.C0())
	lcm.SetC1(chord.C1() | other.C1())
	lcm.SetC2(chord.C2() | other.C2())
	lcm.SetC3(chord.C3() | other.C3())
	lcm.SetC4((chord.C4() | other.C4()) & 1)

	return lcm
}

/*
MustNewChord allocates a fresh zero-valued Chord, panicking on allocation failure.
*/
func MustNewChord() Chord {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	chord, err := NewChord(seg)
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	return chord
}

/*
XOR returns the element-wise XOR of two chords (for cancellative superposition).
*/
func (chord Chord) XOR(other Chord) Chord {
	xor := MustNewChord()

	xor.SetC0(chord.C0() ^ other.C0())
	xor.SetC1(chord.C1() ^ other.C1())
	xor.SetC2(chord.C2() ^ other.C2())
	xor.SetC3(chord.C3() ^ other.C3())
	xor.SetC4((chord.C4() ^ other.C4()) & 1)

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

	if p == nil {
		for i := range chords {
			out[i] = chords[i].Flatten()
		}
		return out
	}

	wg := sync.WaitGroup{}

	for i := range chords {
		idx := i
		resCh := p.Schedule(fmt.Sprintf("flatten-%d", idx), func(ctx context.Context) (any, error) {
			out[idx] = chords[idx].Flatten()
			return nil, nil
		})
		if resCh == nil {
			out[idx] = chords[idx].Flatten()
			continue
		}
		wg.Add(1)
		go func(ch chan *pool.Result) {
			defer wg.Done()
			<-ch
		}(resCh)
	}

	wg.Wait()
	return out
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
	out := MustNewChord()
	shift = shift % logicalBits

	// Fast sparse-array permutation within the 257-bit logical width
	for i := range config.ChordBlocks {
		block := chord.block(i)
		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

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
Rotate3D applies all three GF(257) axes in sequence.
X (Translation): p → (p + 1) mod 257
Y (Dilation):    p → (3·p) mod 257
Z (Affine):      p → (3·p + 1) mod 257
Combined orbit ~17M states. 3 is a primitive root of 257.
*/
func (chord *Chord) Rotate3D() Chord {
	const logicalBits = 257

	out := MustNewChord()

	for i := range config.ChordBlocks {
		block := chord.block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < logicalBits {
				p := (primeIdx + 1) % logicalBits
				p = (3 * p) % logicalBits
				p = (3*p + 1) % logicalBits

				out.Set(p)
			}

			block &= block - 1
		}
	}

	return out
}
