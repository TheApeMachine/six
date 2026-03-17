package data

import (
	"github.com/theapemachine/six/pkg/numeric"
)

const (
	affineFieldMask          = uint64(0x1FF)
	affineWordShiftScale     = 0
	affineWordShiftTranslate = 9
	affineWordMaskScale      = affineFieldMask << affineWordShiftScale
	affineWordMaskTranslate  = affineFieldMask << affineWordShiftTranslate

	shellWordShiftTrajectoryFrom = 18
	shellWordShiftTrajectoryTo   = 27
	shellWordShiftGuardRadius    = 36
	shellWordShiftRouteHint      = 44
	shellWordShiftFlags          = 52

	shellWordMaskTrajectoryFrom = affineFieldMask << shellWordShiftTrajectoryFrom
	shellWordMaskTrajectoryTo   = affineFieldMask << shellWordShiftTrajectoryTo
	shellWordMaskGuardRadius    = uint64(0xFF) << shellWordShiftGuardRadius
	shellWordMaskRouteHint      = uint64(0xFF) << shellWordShiftRouteHint
	shellWordMaskFlags          = uint64(0xFFF) << shellWordShiftFlags
)

const (
	ValueFlagTrajectory uint16 = 1 << iota
	ValueFlagRouteHint
	ValueFlagGuard
	ValueFlagMutable
)

func normalizePhaseWord(phase numeric.Phase) uint64 {
	return uint64(uint32(phase) % numeric.FermatPrime)
}

/*
SetStatePhase records the logical GF(257) state in both the core bit-field and
in ResidualCarry. Fresh native values should use this helper instead of setting
raw bits directly so the state/control split stays explicit.
*/
func (value *Value) SetStatePhase(phase numeric.Phase) {
	if phase == 0 {
		value.SetResidualCarry(0)
		return
	}

	value.Set(int(phase))
	value.SetResidualCarry(uint64(phase))
}

/*
SetAffine stores a tiny affine operator f(x) = ax + b (mod 257) in the shell.
This lets each value behave like a local transition rule rather than a passive
payload. Scale zero is normalized to the identity because traversal wants an
invertible default, not a black hole.
*/
func (value *Value) SetAffine(scale, translate numeric.Phase) {
	scaleWord := normalizePhaseWord(scale)

	if scaleWord == 0 {
		scaleWord = 1
	}

	translateWord := normalizePhaseWord(translate)

	word := value.C7()
	word &^= affineWordMaskScale | affineWordMaskTranslate
	word |= scaleWord << affineWordShiftScale
	word |= translateWord << affineWordShiftTranslate
	value.SetC7(word)
}

/*
Affine retrieves the affine operator stored in the shell. Missing scale data is
interpreted as identity so older values without shell operators remain valid.
*/
func (value *Value) Affine() (numeric.Phase, numeric.Phase) {
	scale := numeric.Phase((value.C7() & affineWordMaskScale) >> affineWordShiftScale)

	if scale == 0 {
		scale = 1
	}

	translate := numeric.Phase((value.C7() & affineWordMaskTranslate) >> affineWordShiftTranslate)

	return scale, translate
}

/*
HasAffine reports whether the value explicitly carries an affine operator.
Legacy values without a stored scale/translate return false.
*/
func (value *Value) HasAffine() bool {
	return value.C7()&(affineWordMaskScale|affineWordMaskTranslate) != 0
}

/*
ApplyAffinePhase advances a phase through the value's local affine operator.
*/
func (value *Value) ApplyAffinePhase(phase numeric.Phase) numeric.Phase {
	scale, translate := value.Affine()
	return numeric.Phase((uint32(scale)*uint32(phase) + uint32(translate)) % numeric.FermatPrime)
}

/*
ApplyAffineValue applies an affine operator (scale, translate) in GF(257) to the
Value's RotationSeed space, producing a new Value with the transformed state imprinted.
*/
func (value Value) ApplyAffineValue(scale, translate numeric.Phase) Value {
	seedScale, seedTranslate := value.RotationSeed()

	combinedScale := (uint32(seedScale)*uint32(scale) + uint32(seedTranslate)*uint32(translate)) % numeric.FermatPrime
	if combinedScale == 0 {
		combinedScale = 1
	}

	result := value
	result.SetStatePhase(numeric.Phase(combinedScale))

	return result
}

/*
OperatorFlags exposes the shell-level execution flags packed into the upper 12
bits of the affine word.
*/
func (value *Value) OperatorFlags() uint16 {
	return uint16((value.C7() & shellWordMaskFlags) >> shellWordShiftFlags)
}

// setOperatorFlags masks and writes the lower 12 operator flag bits into C7
// using shellWordMaskFlags and shellWordShiftFlags.
func (value *Value) setOperatorFlags(flags uint16) {
	word := value.C7()
	word &^= shellWordMaskFlags
	word |= uint64(flags&0x0FFF) << shellWordShiftFlags
	value.SetC7(word)
}

// setOperatorFlag toggles a single flag by reading OperatorFlags then calling setOperatorFlags.
func (value *Value) setOperatorFlag(flag uint16, enabled bool) {
	flags := value.OperatorFlags()

	if enabled {
		flags |= flag
	} else {
		flags &^= flag
	}

	value.setOperatorFlags(flags)
}

/*
HasOperatorFlag reports whether the requested shell-level execution flag is set.
*/
func (value *Value) HasOperatorFlag(flag uint16) bool {
	return value.OperatorFlags()&flag != 0
}

/*
SetMutable marks the value as logically mutable. This does not mutate storage in
place; it merely records that the operator may be versioned append-only in the
LSM when updated.
*/
func (value *Value) SetMutable(mutable bool) {
	value.setOperatorFlag(ValueFlagMutable, mutable)
}

/*
Mutable reports whether the value has been marked as logically mutable.
*/
func (value *Value) Mutable() bool {
	return value.HasOperatorFlag(ValueFlagMutable)
}

/*
SetTrajectory stores a phase-to-phase snapshot of the operator's intended
continuation. This lets traversal prefer an observed local orbit over a generic
affine extrapolation when the current state still matches the stored source.
*/
func (value *Value) SetTrajectory(from, to numeric.Phase) {
	word := value.C7()
	word &^= shellWordMaskTrajectoryFrom | shellWordMaskTrajectoryTo
	word |= normalizePhaseWord(from) << shellWordShiftTrajectoryFrom
	word |= normalizePhaseWord(to) << shellWordShiftTrajectoryTo
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagTrajectory, true)
}

/*
Trajectory retrieves the stored phase snapshot. It returns ok=false when the
value does not explicitly carry a trajectory snapshot in its shell.
*/
func (value *Value) Trajectory() (numeric.Phase, numeric.Phase, bool) {
	if !value.HasTrajectory() {
		return 0, 0, false
	}

	from := numeric.Phase((value.C7() & shellWordMaskTrajectoryFrom) >> shellWordShiftTrajectoryFrom)
	to := numeric.Phase((value.C7() & shellWordMaskTrajectoryTo) >> shellWordShiftTrajectoryTo)

	return from, to, true
}

/*
HasTrajectory reports whether the value carries an explicit trajectory snapshot.
*/
func (value *Value) HasTrajectory() bool {
	return value.HasOperatorFlag(ValueFlagTrajectory)
}

/*
SetGuardRadius stores a tolerated modular phase drift for the next hop. A guard
radius of zero means the operator expects exact continuation.
*/
func (value *Value) SetGuardRadius(radius uint8) {
	word := value.C7()
	word &^= shellWordMaskGuardRadius
	word |= uint64(radius) << shellWordShiftGuardRadius
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagGuard, true)
}

/*
GuardRadius retrieves the stored modular drift budget for the next hop.
*/
func (value *Value) GuardRadius() uint8 {
	return uint8((value.C7() & shellWordMaskGuardRadius) >> shellWordShiftGuardRadius)
}

/*
HasGuard reports whether the value explicitly carries a transition guard.
*/
func (value *Value) HasGuard() bool {
	return value.HasOperatorFlag(ValueFlagGuard)
}

/*
SetRouteHint stores a compact route class that biases the next hop toward
compatible continuation cells without redundantly storing a lexical byte.
*/
func (value *Value) SetRouteHint(route uint8) {
	word := value.C7()
	word &^= shellWordMaskRouteHint
	word |= uint64(route) << shellWordShiftRouteHint
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagRouteHint, true)
}

/*
RouteHint retrieves the shell-level continuation class carried by the value.
*/
func (value *Value) RouteHint() uint8 {
	return uint8((value.C7() & shellWordMaskRouteHint) >> shellWordShiftRouteHint)
}

/*
HasRouteHint reports whether the value carries a route hint.
*/
func (value *Value) HasRouteHint() bool {
	return value.HasOperatorFlag(ValueFlagRouteHint)
}

/*
SetLexicalTransition installs the native operator that would be induced by the
next byte under the original lexical phase rule. This preserves compatibility
with existing prompt/state logic while keeping the stored value lexical-free.

When the current state snapshot is already available in ResidualCarry, the value
also records a trajectory snapshot and a compact route hint in the shell so the
operator can steer traversal without replaying lexical identity from storage.
*/
func (value *Value) SetLexicalTransition(next byte) {
	value.SetAffine(PhaseScaleForByte(next), 0)
	value.SetRouteHint(RouteHintForSymbol(next))

	if carry := value.ResidualCarry(); carry > 0 {
		current := numeric.Phase(carry % uint64(numeric.FermatPrime))

		if current > 0 {
			value.SetTrajectory(current, value.ApplyAffinePhase(current))
		}
	}
}
