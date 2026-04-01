package firmware

import (
	"math"
	"math/bits"
	"math/rand"
	"sort"

	"github.com/theapemachine/six/pkg/core"
)

/*
hieCodebookNibble holds one collision-free byte codebook entry per nibble row;
EncodeHIE places each byte at hieBandPerm[n]*8 so lanes are affine-interleaved.
*/
var hieCodebookNibble [8][16]uint64

/*
hieBandPerm maps logical nibble index n to a byte lane (0..7) via affine order
(27*n) mod 61 ranked among nibbles — interleaves bands so crossover noise
spreads across all fields instead of wiping adjacent instruction strips.
*/
var hieBandPerm [8]int

const (
	hieThirdParentPrime  = uint64(4294967291)
	hieThirdParentWeight = uint64(1024)
)

/*
init fills hieCodebookNibble with collision-free low-byte patterns per nibble
row and builds hieBandPerm from (27*n) mod 61.
*/
func init() {
	rng := rand.New(rand.NewSource(42))

	var takenByte [8][256]bool

	for nibbleIdx := 0; nibbleIdx < 8; nibbleIdx++ {
		currentHV := uint64(rng.Intn(256))

		for val := 0; val < 16; val++ {
			byteVal := byte(currentHV & 0xFF)

			for takenByte[nibbleIdx][byteVal] {
				currentHV ^= uint64(1) << rng.Intn(8)
				currentHV &= 0xFF
				byteVal = byte(currentHV & 0xFF)
			}

			takenByte[nibbleIdx][byteVal] = true
			hieCodebookNibble[nibbleIdx][val] = uint64(byteVal)

			flipLow := uint64(1) << rng.Intn(8)
			flipHigh := uint64(1) << rng.Intn(8)
			currentHV ^= flipLow | flipHigh
			currentHV &= 0xFF
		}
	}

	type nibbleOrder struct {
		nibble int
		affine int
	}
	orders := make([]nibbleOrder, 8)
	for n := 0; n < 8; n++ {
		orders[n] = nibbleOrder{nibble: n, affine: (27 * n) % 61}
	}
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].affine != orders[j].affine {
			return orders[i].affine < orders[j].affine
		}

		return orders[i].nibble < orders[j].nibble
	})

	for rank, entry := range orders {
		hieBandPerm[entry.nibble] = rank
	}
}

/*
ProgramPayloadFirst32BitSlot is the first 32-bit instruction index after the
bootstrap prefix (PayloadProgramWordOffset words × two lanes per word).
*/
func ProgramPayloadFirst32BitSlot() int {
	return int(core.PayloadProgramWordOffset) * 2
}

/*
program32BitSlotCount returns how many 32-bit InstructionSlot indices fit in the
configured program region (two per program uint64).
*/
func program32BitSlotCount() int {
	nWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)

	return nWords * 2
}

/*
EncodeHIE maps one discrete 32-bit LGP instruction slot payload into a 64-bit
spatially multiplexed hypervector.
*/
func EncodeHIE(instr uint32) uint64 {
	var hv uint64

	for n := 0; n < 8; n++ {
		nibbleVal := (instr >> (n * 4)) & 0xF
		band := hieBandPerm[n]
		hv |= (hieCodebookNibble[n][nibbleVal] & 0xFF) << (band * 8)
	}

	return hv
}

/*
DecodeHIE snaps a fuzzy multiplexed vector to the nearest valid codebook entry
per 8-bit band and returns the reconstructed 32-bit instruction.
*/
func DecodeHIE(hv uint64) uint32 {
	var instr uint32

	for n := 0; n < 8; n++ {
		band := hieBandPerm[n]
		segment := byte((hv >> (band * 8)) & 0xFF)
		bestDist := 9
		bestVal := uint32(0)

		for v := 0; v < 16; v++ {
			cbByte := byte(hieCodebookNibble[n][v] & 0xFF)
			dist := bits.OnesCount8(segment ^ cbByte)
			if dist < bestDist || (dist == bestDist && v < int(bestVal)) {
				bestDist = dist
				bestVal = uint32(v)
			}
		}

		instr |= bestVal << (n * 4)
	}

	return instr
}

/*
MajorityRuleBitwise blends three uint64 parents with per-bit majority (2-of-3).
*/
func MajorityRuleBitwise(parentA, parentB, noise uint64) uint64 {
	return (parentA & parentB) | (parentA & noise) | (parentB & noise)
}

func clampParentBias(parentBias float64) float64 {
	if math.IsNaN(parentBias) {
		return 0
	}

	if parentBias <= 0 {
		return 0
	}

	if parentBias >= 1 {
		return 1
	}

	return parentBias
}

func hieBlendAffineField(affineValue uint32, donorValue uint32, parentBias float64, modulus uint32) uint32 {
	if modulus == 0 {
		return 0
	}

	if parentBias <= 0 {
		return affineValue % modulus
	}

	if parentBias >= 1 {
		return donorValue % modulus
	}

	weight := uint64(parentBias*float64(hieThirdParentWeight) + 0.5)
	if weight > hieThirdParentWeight {
		weight = hieThirdParentWeight
	}

	affine := uint64(affineValue % modulus)
	donor := uint64(donorValue % modulus)
	mixed := ((hieThirdParentWeight-weight)*affine + weight*donor + hieThirdParentWeight/2) / hieThirdParentWeight

	return uint32(mixed % uint64(modulus))
}

func hieAffineThirdInstruction(slot int, instrDonorA uint32, instrDonorB uint32, seed uint64, parentBias float64) uint32 {
	slotIndex := uint64(slot + 1)

	weight := uint64(parentBias*float64(hieThirdParentWeight) + 0.5)
	multiplier := ((seed | 1) + (weight << 1) + slotIndex) % hieThirdParentPrime
	if multiplier < 3 {
		multiplier += 3
	}
	if multiplier%2 == 0 {
		multiplier++
	}

	offset := (seed + slotIndex*0x9E3779B97F4A7C15) % hieThirdParentPrime
	value := (multiplier*slotIndex + offset) % hieThirdParentPrime

	affineOp := uint32(value & 0xF)
	affineSrc := uint32((value >> 4) & 0x7F)
	affineDst := uint32((bits.RotateLeft64(value, 13) >> 11) & 0x7F)

	donorOp := instrDonorA & 0xF
	donorSrc := (instrDonorA >> 4) & 0x7F
	donorDst := (instrDonorA >> 18) & 0x7F

	op := hieBlendAffineField(affineOp, donorOp, parentBias, 16)
	src := hieBlendAffineField(affineSrc, donorSrc, parentBias, 128)
	dst := hieBlendAffineField(affineDst, donorDst, parentBias, 128)

	return op | (src << 4) | (dst << 18)
}

/*
hieNoiseThirdParent draws a structured third parent for majority blend.

parentBias in [0, 1]: at 0, the third parent is a pure affine instruction-space
orbit keyed by slot and donor fields (maximum structured exploration). At 1,
the orbit collapses to donor A’s exact slot (maximum exploit). Values in between
interpolate the affine opcode/src/dst fields back toward donor A before HIE
encoding so the majority vote stays valid while preserving a regular schedule.
*/
func hieNoiseThirdParent(slot int, instrDonorA uint32, instrDonorB uint32, rng *rand.Rand, parentBias float64) uint64 {
	if rng == nil {
		return 0
	}

	parentBias = clampParentBias(parentBias)
	if parentBias >= 1 {
		return EncodeHIE(instrDonorA)
	}

	seed := uint64(rng.Uint32())
	seed ^= uint64(bits.RotateLeft32(instrDonorA^bits.RotateLeft32(instrDonorB, 11), slot&31))

	collapseThreshold := uint64(parentBias * float64(hieThirdParentWeight) * 0.5)
	if collapseThreshold > 0 && seed%hieThirdParentWeight < collapseThreshold {
		return EncodeHIE(instrDonorA)
	}

	thirdParent := hieAffineThirdInstruction(slot, instrDonorA, instrDonorB, seed, parentBias)

	return EncodeHIE(thirdParent)
}

/*
HolographicCrossover writes into recipient the per-slot majority blend of donorA,
donorB, and noise in HIE space, then decodes to executable 32-bit slots.

parentBias steers the third parent toward donor A’s hypervector (substrate
exploit); 0 preserves the original fully random third parent.

Bootstrap program words (first PayloadProgramWordOffset uint64s) are left
unchanged so installed firmware entrypoints stay intact.

If donorA or donorB is nil, the function returns without writing.
*/
func HolographicCrossover(recipient, donorA, donorB *[128]uint64, rng *rand.Rand, parentBias float64) {
	if recipient == nil || donorA == nil || donorB == nil || rng == nil {
		return
	}

	parentBias = clampParentBias(parentBias)

	firstEvolved := ProgramPayloadFirst32BitSlot()
	numSlots := program32BitSlotCount()

	for slot := firstEvolved; slot < numSlots; slot++ {
		instrA := InstructionSlot(donorA, slot)
		instrB := InstructionSlot(donorB, slot)
		hvA := EncodeHIE(instrA)
		hvB := EncodeHIE(instrB)
		noise := hieNoiseThirdParent(slot, instrA, instrB, rng, parentBias)
		childHV := MajorityRuleBitwise(hvA, hvB, noise)
		childInstr := DecodeHIE(childHV)
		SetInstructionSlot(recipient, slot, childInstr)
	}
}

/*
HolographicCrossoverTwoParent blends the recipient's current payload program with
donor's payload (recipient as first parent, donor as second). Useful where only
one mate exists; parentBias applies the same third-parent steering as
HolographicCrossover.
*/
func HolographicCrossoverTwoParent(recipient, donor *[128]uint64, rng *rand.Rand, parentBias float64) {
	if recipient == nil || donor == nil || rng == nil {
		return
	}

	HolographicCrossover(recipient, recipient, donor, rng, parentBias)
}
