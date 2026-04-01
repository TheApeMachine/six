package firmware

import (
	"fmt"
	"math"

	"github.com/theapemachine/six/pkg/core"
)

/*
AffineSlotShuffleModulus matches the fixed-size branchless pipeline narrative
(52 words of program storage); LGP still indexes via Program.Bits but NOP
shatter uses this modulus for the affine slot permutation the FSM spec calls for.
*/
const AffineSlotShuffleModulus = 52

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}

	if b < 0 {
		b = -b
	}

	for b != 0 {
		a, b = b, a%b
	}

	return a
}

func AffineCoprimeMultiplier(affineA, modulus int) int {
	if modulus <= 1 {
		return 1
	}

	affineA %= modulus
	if affineA < 0 {
		affineA += modulus
	}
	if affineA == 0 {
		affineA = 1
	}

	for gcdInt(affineA, modulus) != 1 {
		affineA++
		if affineA >= modulus {
			affineA = 1
		}
	}

	return affineA
}

/*
AffineUnrollSlots builds branchless unrolled slot payloads without DJNZ:

	instr[i] = baseOpcode | (((stride*i) mod maxWord) << wordShift)

maxWord must be >= 1. Feeds the same ScanStride family used in ScanSignals so
generated loads track affine token order.
*/
func AffineUnrollSlots(baseOpcode uint32, stride uint64, maxWord int, wordShift uint, numSlots int) []uint32 {
	if numSlots <= 0 {
		return nil
	}

	if maxWord <= 0 {
		maxWord = 1
	}

	mw := uint64(maxWord)
	out := make([]uint32, numSlots)
	shift := wordShift % 32

	for idx := 0; idx < numSlots; idx++ {
		w := uint32((stride * uint64(idx)) % mw)
		out[idx] = baseOpcode | (w << shift)
	}

	return out
}

/*
ApplyAffineUnrollToProgram writes unrolled 32-bit LGP payloads into c starting at slot startSlot.
*/
func ApplyAffineUnrollToProgram(c *[128]uint64, startSlot int, baseOpcode uint32, stride uint64, maxWord int, wordShift uint, numSlots int) {
	if c == nil || numSlots <= 0 {
		return
	}

	slots := AffineUnrollSlots(baseOpcode, stride, maxWord, wordShift, numSlots)

	for idx := range slots {
		SetInstructionSlot(c, startSlot+idx, slots[idx])
	}
}

/*
AffineNextProgramID maps the StateAccumulator word through an affine step for
follow-up program selection:

	nextID = (multiplier*stateAccumulator + offset) mod totalPrograms
*/
func AffineNextProgramID(stateAccumulator, multiplier, offset uint64, totalPrograms int) uint64 {
	if totalPrograms <= 0 {
		return 0
	}

	tp := uint64(totalPrograms)

	return (multiplier*stateAccumulator + offset) % tp
}

/*
NOPShatterLGP permutes payload LGP slots with newIdx = (affineA * oldIdx) mod modulus.
If modulus <= 0, AffineSlotShuffleModulus is used. Collisions are possible when
gcd(affineA, modulus) != 1; callers pick coprime multipliers for bijections.
*/
func NOPShatterLGP(c *[128]uint64, affineA, modulus int) {
	if c == nil || affineA < 0 {
		return
	}

	first := ProgramPayloadFirst32BitSlot()
	last := program32BitSlotCount()
	if first >= last {
		return
	}

	n := last - first
	if modulus <= 0 {
		modulus = AffineSlotShuffleModulus
	}

	if n > modulus {
		n = modulus
	}

	effectiveA := affineA % modulus
	if effectiveA < 0 {
		effectiveA += modulus
	}

	if effectiveA == 0 || gcdInt(effectiveA, modulus) != 1 {
		panic(fmt.Sprintf("firmware.NOPShatterLGP: affineA=%d modulus=%d is not bijective", affineA, modulus))
	}

	old := make([]uint32, n)

	for idx := 0; idx < n; idx++ {
		old[idx] = InstructionSlot(c, first+idx)
	}

	shattered := make([]uint32, n)

	for idx := 0; idx < n; idx++ {
		dst := (effectiveA * idx) % modulus
		if dst >= n {
			dst = dst % n
		}

		shattered[dst] = old[idx]
	}

	for idx := 0; idx < n; idx++ {
		SetInstructionSlot(c, first+idx, shattered[idx])
	}
}

/*
HolographicScheduleSignature mixes a plain next-program id with substrate fitness
and signal stride so queue routing reflects what the frame “knows” about its data.

	next XOR fold(Float64bits(score)) XOR (scanStride * golden)
*/
func HolographicScheduleSignature(nextProgramID uint64, substrateExploitScore float64, scanStride uint64) uint64 {
	const golden = uint64(0x9E3779B97F4A7C15)

	scoreMix := uint64(0)
	if !math.IsNaN(substrateExploitScore) {
		scoreMix = math.Float64bits(substrateExploitScore)
	}

	return nextProgramID ^ (scoreMix >> 1) ^ (scanStride * golden)
}

/*
PayloadLGPSpan returns the inclusive half-open [first, last) slot indices for the
evolvable LGP payload (after bootstrap).
*/
func PayloadLGPSpan() (first int, last int) {
	first = ProgramPayloadFirst32BitSlot()
	last = program32BitSlotCount()

	return
}

/*
AffinePipelineWordCount is the number of uint64 program words in the configured
region (Region.Program.Bits / 64), for doc / viz parity with “52 words”.
*/
func AffinePipelineWordCount() int {
	if core.Cfg == nil {
		return AffineSlotShuffleModulus
	}

	bits := int(core.Cfg.Value.Region.Program.Bits)
	if bits <= 0 {
		return AffineSlotShuffleModulus
	}

	return (bits + 63) / 64
}
