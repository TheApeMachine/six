package lsm

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
extractStatePhase recovers the GF(257) state encoded in a stored state chord.
ResidualCarry is treated as the authoritative snapshot when present because the
stored value is now allowed to be lexical-free while query observables still
carry the transient five-bit seed. When that snapshot is absent, we fall back to
ignoring the lexical seed bits and reading whatever native state bit remains.
*/
func extractStatePhase(chord data.Chord, symbol byte) (numeric.Phase, bool) {
	if carry := chord.ResidualCarry(); carry > 0 {
		phase := numeric.Phase(carry % uint64(numeric.FermatPrime))
		if phase > 0 {
			return phase, true
		}
	}

	base := data.BaseChord(symbol)

	for blockIdx := 0; blockIdx < 5; blockIdx++ {
		block := chord.Block(blockIdx)
		if blockIdx == 4 {
			block &= 1
		}
		block &^= base.Block(blockIdx)
		if block == 0 {
			continue
		}

		bitIdx := bits.TrailingZeros64(block)
		primeIdx := blockIdx*64 + bitIdx
		phase := numeric.Phase(primeIdx)
		if phase >= 1 && uint32(phase) < numeric.FermatPrime {
			return phase, true
		}
	}

	return 0, false
}

func statePhaseMatches(chord data.Chord, symbol byte, expected numeric.Phase) bool {
	phase, ok := extractStatePhase(chord, symbol)
	return ok && phase == expected
}

func phaseDistanceMod257(left, right numeric.Phase) uint32 {
	delta := int32(left) - int32(right)
	if delta < 0 {
		delta = -delta
	}
	if delta > int32(numeric.FermatPrime)/2 {
		delta = int32(numeric.FermatPrime) - delta
	}
	return uint32(delta)
}

func operatorRoutePenalty(value data.Chord, nextSymbol byte) int {
	if !value.HasRouteHint() {
		return 0
	}
	return bits.OnesCount8(value.RouteHint()^data.RouteHintForSymbol(nextSymbol)) * 2
}

func operatorPhaseAcceptance(
	value data.Chord,
	expected numeric.Phase,
	observed numeric.Phase,
) (numeric.Phase, int, bool) {
	if observed == expected {
		return observed, 0, true
	}

	if !value.HasGuard() {
		return 0, 0, false
	}

	drift := phaseDistanceMod257(expected, observed)
	if drift > uint32(value.GuardRadius()) {
		return 0, 0, false
	}

	return observed, int(drift), true
}

/*
advanceProgramPosition returns the next boundary-local depth implied by a stored
native value. The value's program shell is authoritative: reset returns to local
depth 0, jump advances by the encoded distance, halt/terminal stop traversal,
and ordinary values fall through to the next local depth.
*/
func advanceProgramPosition(pos uint32, value data.Chord) (uint32, bool) {
	if value.Terminal() || value.Opcode() == uint64(data.OpcodeHalt) {
		return 0, false
	}

	if data.Opcode(value.Opcode()) == data.OpcodeReset {
		return 0, true
	}

	if jump := value.Jump(); jump > 0 {
		return pos + jump, true
	}

	return pos + 1, true
}

/*
advanceProgramCursor applies the stored native transition while also tracking
boundary-reset segment changes. Resets return to local depth 0 and increment the
segment; jumps and ordinary next-steps preserve the current segment; halt stops
traversal entirely.
*/
func advanceProgramCursor(pos, segment uint32, value data.Chord) (uint32, uint32, bool) {
	nextPos, ok := advanceProgramPosition(pos, value)
	if !ok {
		return 0, segment, false
	}

	if data.Opcode(value.Opcode()) == data.OpcodeReset {
		return 0, segment + 1, true
	}

	return nextPos, segment, true
}

/*
predictNextPhaseFromValue advances a GF(257) state through the stored value's
native operator. Values that carry an explicit trajectory snapshot use that
continuation first when the current phase is still close to the stored source.
Otherwise traversal falls back to the affine shell operator and finally to the
legacy lexical primitive-root rule.
*/
func predictNextPhaseFromValue(
	calc *numeric.Calculus,
	value data.Chord,
	current numeric.Phase,
	nextSymbol byte,
) numeric.Phase {
	if from, to, ok := value.Trajectory(); ok {
		if from == current && to != 0 {
			return to
		}

		if value.HasGuard() && to != 0 && phaseDistanceMod257(from, current) <= uint32(value.GuardRadius()) {
			return to
		}
	}

	if value.HasAffine() {
		if next := value.ApplyAffinePhase(current); next != 0 {
			return next
		}
	}

	return calc.Multiply(
		current,
		calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(nextSymbol)),
	)
}

func firstMetaForKeyUnsafe(idx *SpatialIndexServer, key uint64) data.Chord {
	meta := data.MustNewChord()
	if metas := idx.metaEntries[key]; len(metas) > 0 {
		meta.CopyFrom(metas[0])
	}
	return meta
}

func lexicalDistance(left, right byte) int {
	return bits.OnesCount8(left ^ right)
}
