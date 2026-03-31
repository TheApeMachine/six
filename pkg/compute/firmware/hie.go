package firmware

import (
	"math/bits"
	"math/rand"

	"github.com/theapemachine/six/pkg/core"
)

/*
hieCodebookNibble holds spatially multiplexed 8-bit hypervector strips for each
nibble position of a 32-bit LGP instruction slot. Non-overlapping 8-bit bands
inside a uint64 let majority-rule blending act on each nibble independently
without XOR-binding collision across fields.
*/
var hieCodebookNibble [8][16]uint64

/*
init fills hieCodebookNibble with collision-free 8-bit strips per row so each
nibble value maps to a unique band pattern; without that, nearest-neighbor
decode could tie and break EncodeHIE round-trips.
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
			hieCodebookNibble[nibbleIdx][val] = uint64(byteVal) << (nibbleIdx * 8)

			flipLow := uint64(1) << rng.Intn(8)
			flipHigh := uint64(1) << rng.Intn(8)
			currentHV ^= flipLow | flipHigh
			currentHV &= 0xFF
		}
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
		hv |= hieCodebookNibble[n][nibbleVal]
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
		segment := byte((hv >> (n * 8)) & 0xFF)
		bestDist := 9
		bestVal := uint32(0)

		for v := 0; v < 16; v++ {
			cbByte := byte((hieCodebookNibble[n][v] >> (n * 8)) & 0xFF)
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

/*
HolographicCrossover writes into recipient the per-slot majority blend of donorA,
donorB, and noise in HIE space, then decodes to executable 32-bit slots.

Bootstrap program words (first PayloadProgramWordOffset uint64s) are left
unchanged so installed firmware entrypoints stay intact.

If donorA or donorB is nil, the function returns without writing.
*/
func HolographicCrossover(recipient, donorA, donorB *[128]uint64, rng *rand.Rand) {
	if recipient == nil || donorA == nil || donorB == nil || rng == nil {
		return
	}

	firstEvolved := ProgramPayloadFirst32BitSlot()
	numSlots := program32BitSlotCount()

	for slot := firstEvolved; slot < numSlots; slot++ {
		instrA := InstructionSlot(donorA, slot)
		instrB := InstructionSlot(donorB, slot)
		hvA := EncodeHIE(instrA)
		hvB := EncodeHIE(instrB)
		noise := rng.Uint64()
		childHV := MajorityRuleBitwise(hvA, hvB, noise)
		childInstr := DecodeHIE(childHV)
		SetInstructionSlot(recipient, slot, childInstr)
	}
}

/*
HolographicCrossoverTwoParent blends the recipient's current payload program with
donor's payload (recipient as first parent, donor as second). Useful where only
one mate exists; still uses bitwise noise as the third voter.
*/
func HolographicCrossoverTwoParent(recipient, donor *[128]uint64, rng *rand.Rand) {
	if recipient == nil || donor == nil || rng == nil {
		return
	}

	HolographicCrossover(recipient, recipient, donor, rng)
}
