package primitive

import (
	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
Shell word layout for GF(8191).

The shell occupies blocks[CoreBlocks..TotalBlocks-1]. The primary shell
word is the last block (block index TotalBlocks-1), which packs:

	bits  0..12:  affine scale      (13 bits, covers 0..8190)
	bits 13..25:  affine translate  (13 bits)
	bits 26..38:  trajectory from   (13 bits)
	bits 39..51:  trajectory to     (13 bits)
	bits 52..59:  guard radius      (8 bits)
	bits 60..63:  flags             (4 bits)

The residual/carry word is block[CoreBlocks] (shell block 0).
The opcode word is block[CoreBlocks+1] (shell block 1).
*/
const (
	affineFieldMask          = uint64(0x1FFF)
	affineWordShiftScale     = 0
	affineWordShiftTranslate = 13
	affineWordMaskScale      = affineFieldMask << affineWordShiftScale
	affineWordMaskTranslate  = affineFieldMask << affineWordShiftTranslate

	shellWordShiftTrajectoryFrom = 26
	shellWordShiftTrajectoryTo   = 39
	shellWordShiftGuardRadius    = 52
	shellWordShiftFlags          = 60

	shellWordMaskTrajectoryFrom = affineFieldMask << shellWordShiftTrajectoryFrom
	shellWordMaskTrajectoryTo   = affineFieldMask << shellWordShiftTrajectoryTo
	shellWordMaskGuardRadius    = uint64(0xFF) << shellWordShiftGuardRadius
	shellWordMaskFlags          = uint64(0xF) << shellWordShiftFlags

	shellWordBlock = config.TotalBlocks - 1
)

const (
	ValueFlagTrajectory uint16 = 1 << iota
	ValueFlagRouteHint
	ValueFlagGuard
	ValueFlagMutable
)

/*
SetStatePhase records the logical GF(8191) state in both the core bit-field and
in ResidualCarry. Fresh native values should use this helper instead of setting
raw bits directly so the state/control split stays explicit.
*/
func (value Value) SetStatePhase(phase numeric.Phase) {
	if phase == 0 {
		value.SetResidualCarry(0)
		return
	}

	value.Set(int(phase))
	value.SetResidualCarry(uint64(phase))
}

/*
SetAffine stores a tiny affine operator f(x) = ax + b (mod 8191) in the shell.
Scale zero is normalized to the identity because traversal wants an
invertible default, not a black hole.
*/
func (value Value) SetAffine(scale, translate numeric.Phase) {
	scaleWord := value.normalizePhaseWord(scale)

	if scaleWord == 0 {
		scaleWord = 1
	}

	translateWord := value.normalizePhaseWord(translate)

	word := value.Block(shellWordBlock)
	word &^= affineWordMaskScale | affineWordMaskTranslate
	word |= scaleWord << affineWordShiftScale
	word |= translateWord << affineWordShiftTranslate
	value.setBlock(shellWordBlock, word)
}

/*
Affine retrieves the affine operator stored in the shell. Missing scale data is
interpreted as identity so older values without shell operators remain valid.
*/
func (value *Value) Affine() (numeric.Phase, numeric.Phase) {
	word := value.Block(shellWordBlock)

	scale := numeric.Phase(
		(word & affineWordMaskScale) >> affineWordShiftScale,
	)

	if scale == 0 {
		scale = 1
	}

	translate := numeric.Phase(
		(word & affineWordMaskTranslate) >> affineWordShiftTranslate,
	)

	return scale, translate
}

/*
SetTrajectory stores a phase-to-phase snapshot of the operator's intended
continuation.
*/
func (value Value) SetTrajectory(from, to numeric.Phase) {
	word := value.Block(shellWordBlock)
	word &^= shellWordMaskTrajectoryFrom | shellWordMaskTrajectoryTo
	word |= value.normalizePhaseWord(from) << shellWordShiftTrajectoryFrom
	word |= value.normalizePhaseWord(to) << shellWordShiftTrajectoryTo
	value.setBlock(shellWordBlock, word)
	value.setOperatorFlag(ValueFlagTrajectory, true)
}

/*
Trajectory retrieves the stored phase snapshot. Returns ok=false when the
value does not explicitly carry a trajectory snapshot.
*/
func (value Value) Trajectory() (numeric.Phase, numeric.Phase, bool) {
	if !value.HasTrajectory() {
		return 0, 0, false
	}

	word := value.Block(shellWordBlock)
	from := numeric.Phase((word & shellWordMaskTrajectoryFrom) >> shellWordShiftTrajectoryFrom)
	to := numeric.Phase((word & shellWordMaskTrajectoryTo) >> shellWordShiftTrajectoryTo)

	return from, to, true
}

/*
HasTrajectory reports whether the value carries an explicit trajectory snapshot.
*/
func (value Value) HasTrajectory() bool {
	return value.HasOperatorFlag(ValueFlagTrajectory)
}

/*
SetGuardRadius stores a tolerated modular phase drift for the next hop.
*/
func (value Value) SetGuardRadius(radius uint8) {
	word := value.Block(shellWordBlock)
	word &^= shellWordMaskGuardRadius
	word |= uint64(radius) << shellWordShiftGuardRadius
	value.setBlock(shellWordBlock, word)
	value.setOperatorFlag(ValueFlagGuard, true)
}

/*
GuardRadius retrieves the stored modular drift budget for the next hop.
*/
func (value Value) GuardRadius() uint8 {
	return uint8((value.Block(shellWordBlock) & shellWordMaskGuardRadius) >> shellWordShiftGuardRadius)
}

/*
HasGuard reports whether the value explicitly carries a transition guard.
*/
func (value Value) HasGuard() bool {
	return value.HasOperatorFlag(ValueFlagGuard)
}

/*
ApplyAffinePhase advances a phase through the value's local affine operator.
*/
func (value *Value) ApplyAffinePhase(phase numeric.Phase) numeric.Phase {
	scale, translate := value.Affine()

	return numeric.Phase(
		numeric.MersenneReduce(uint32(scale)*uint32(phase) + uint32(translate)),
	)
}

/*
ApplyAffineValue applies an affine operator (scale, translate) in GF(8191) to the
Value's RotationSeed space, producing a new Value with the transformed state imprinted.
*/
func (value Value) ApplyAffineValue(scale, translate numeric.Phase) Value {
	seedScale, seedTranslate := value.RotationSeed()

	combinedScale := numeric.MersenneReduce(
		uint32(seedScale)*uint32(scale) + uint32(seedTranslate)*uint32(translate),
	)

	if combinedScale == 0 {
		combinedScale = 1
	}

	result := value
	result.SetStatePhase(numeric.Phase(combinedScale))

	return result
}

/*
HasAffine reports whether the value explicitly carries an affine operator.
*/
func (value *Value) HasAffine() bool {
	word := value.Block(shellWordBlock)
	return word&(affineWordMaskScale|affineWordMaskTranslate) != 0
}

/*
normalizePhaseWord clamps a phase to the GF(8191) field so shell word packing
does not overflow the 13-bit field.
*/
func (value Value) normalizePhaseWord(phase numeric.Phase) uint64 {
	return uint64(numeric.MersenneReduce(uint32(phase)))
}
