package data

import "github.com/theapemachine/six/pkg/numeric"

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

var (
	bytePhaseScales = initBytePhaseScales()
	byteRouteHints  = initByteRouteHints()
)

func initBytePhaseScales() [256]numeric.Phase {
	var scales [256]numeric.Phase
	for b := 0; b < len(scales); b++ {
		scales[b] = numeric.Phase(modPow257(int(numeric.FermatPrimitive), b))
		if scales[b] == 0 {
			scales[b] = 1
		}
	}
	return scales
}

func initByteRouteHints() [256]uint8 {
	var hints [256]uint8
	for b := 0; b < len(hints); b++ {
		base := BaseChord(byte(b))
		hints[b] = uint8(ChordBin(&base))
	}
	return hints
}

func normalizePhaseWord(phase numeric.Phase) uint64 {
	return uint64(uint32(phase) % numeric.FermatPrime)
}

/*
PhaseScaleForByte returns the multiplicative GF(257) transition induced by a
byte. It is the native phase operator behind the old lexical update rule
state' = state * 3^byte (mod 257), but factored out so stored values can carry
that operator directly without redundantly storing the byte seed.
*/
func PhaseScaleForByte(symbol byte) numeric.Phase {
	return bytePhaseScales[int(symbol)]
}

/*
RouteHintForSymbol returns a compact routing class for a byte without storing
the byte itself in the persistent value. It is derived from the byte's sparse
lexical seed and only used as an execution hint inside the shell.
*/
func RouteHintForSymbol(symbol byte) uint8 {
	return byteRouteHints[int(symbol)]
}

/*
NeutralValue allocates a lexical-free native value. The value starts with an
identity affine operator so it behaves like a tiny local program even before
any lexical observable is projected onto it.
*/
func NeutralValue() Chord {
	value := MustNewChord()
	value.SetAffine(1, 0)
	return value
}

/*
SeedObservable projects a lexical seed onto an otherwise native value so
prompts and human-facing outputs remain decodable without forcing storage to
carry the byte twice.
*/
func SeedObservable(symbol byte, value Chord) Chord {
	return ObservableValue(symbol, value)
}

/*
SetStatePhase records the logical GF(257) state in both the core bit-field and
in ResidualCarry. Fresh native values should use this helper instead of setting
raw bits directly so the state/control split stays explicit.
*/
func (chord *Chord) SetStatePhase(phase numeric.Phase) {
	if phase == 0 {
		chord.SetResidualCarry(0)
		return
	}

	chord.Set(int(phase))
	chord.SetResidualCarry(uint64(phase))
}

/*
SetAffine stores a tiny affine operator f(x) = ax + b (mod 257) in the shell.
This lets each value behave like a local transition rule rather than a passive
payload. Scale zero is normalized to the identity because traversal wants an
invertible default, not a black hole.
*/
func (chord *Chord) SetAffine(scale, translate numeric.Phase) {
	s := normalizePhaseWord(scale)
	if s == 0 {
		s = 1
	}
	t := normalizePhaseWord(translate)

	word := chord.C7()
	word &^= affineWordMaskScale | affineWordMaskTranslate
	word |= s << affineWordShiftScale
	word |= t << affineWordShiftTranslate
	chord.SetC7(word)
}

/*
Affine retrieves the affine operator stored in the shell. Missing scale data is
interpreted as identity so older values without shell operators remain valid.
*/
func (chord *Chord) Affine() (numeric.Phase, numeric.Phase) {
	scale := numeric.Phase((chord.C7() & affineWordMaskScale) >> affineWordShiftScale)
	if scale == 0 {
		scale = 1
	}
	translate := numeric.Phase((chord.C7() & affineWordMaskTranslate) >> affineWordShiftTranslate)
	return scale, translate
}

/*
HasAffine reports whether the value explicitly carries an affine operator.
Legacy values without a stored scale/translate return false.
*/
func (chord *Chord) HasAffine() bool {
	return chord.C7()&(affineWordMaskScale|affineWordMaskTranslate) != 0
}

/*
ApplyAffinePhase advances a phase through the value's local affine operator.
*/
func (chord *Chord) ApplyAffinePhase(phase numeric.Phase) numeric.Phase {
	scale, translate := chord.Affine()
	return numeric.Phase((uint32(scale)*uint32(phase) + uint32(translate)) % numeric.FermatPrime)
}

/*
OperatorFlags exposes the shell-level execution flags packed into the upper 12
bits of the affine word.
*/
func (chord *Chord) OperatorFlags() uint16 {
	return uint16((chord.C7() & shellWordMaskFlags) >> shellWordShiftFlags)
}

func (chord *Chord) setOperatorFlags(flags uint16) {
	word := chord.C7()
	word &^= shellWordMaskFlags
	word |= uint64(flags&0x0FFF) << shellWordShiftFlags
	chord.SetC7(word)
}

func (chord *Chord) setOperatorFlag(flag uint16, enabled bool) {
	flags := chord.OperatorFlags()
	if enabled {
		flags |= flag
	} else {
		flags &^= flag
	}
	chord.setOperatorFlags(flags)
}

/*
HasOperatorFlag reports whether the requested shell-level execution flag is set.
*/
func (chord *Chord) HasOperatorFlag(flag uint16) bool {
	return chord.OperatorFlags()&flag != 0
}

/*
SetMutable marks the value as logically mutable. This does not mutate storage in
place; it merely records that the operator may be versioned append-only in the
LSM when updated.
*/
func (chord *Chord) SetMutable(mutable bool) {
	chord.setOperatorFlag(ValueFlagMutable, mutable)
}

/*
Mutable reports whether the value has been marked as logically mutable.
*/
func (chord *Chord) Mutable() bool {
	return chord.HasOperatorFlag(ValueFlagMutable)
}

/*
SetTrajectory stores a phase-to-phase snapshot of the operator's intended
continuation. This lets traversal prefer an observed local orbit over a generic
affine extrapolation when the current state still matches the stored source.
*/
func (chord *Chord) SetTrajectory(from, to numeric.Phase) {
	word := chord.C7()
	word &^= shellWordMaskTrajectoryFrom | shellWordMaskTrajectoryTo
	word |= normalizePhaseWord(from) << shellWordShiftTrajectoryFrom
	word |= normalizePhaseWord(to) << shellWordShiftTrajectoryTo
	chord.SetC7(word)
	chord.setOperatorFlag(ValueFlagTrajectory, true)
}

/*
Trajectory retrieves the stored phase snapshot. It returns ok=false when the
value does not explicitly carry a trajectory snapshot in its shell.
*/
func (chord *Chord) Trajectory() (numeric.Phase, numeric.Phase, bool) {
	if !chord.HasTrajectory() {
		return 0, 0, false
	}

	from := numeric.Phase((chord.C7() & shellWordMaskTrajectoryFrom) >> shellWordShiftTrajectoryFrom)
	to := numeric.Phase((chord.C7() & shellWordMaskTrajectoryTo) >> shellWordShiftTrajectoryTo)
	return from, to, true
}

/*
HasTrajectory reports whether the value carries an explicit trajectory snapshot.
*/
func (chord *Chord) HasTrajectory() bool {
	return chord.HasOperatorFlag(ValueFlagTrajectory)
}

/*
SetGuardRadius stores a tolerated modular phase drift for the next hop. A guard
radius of zero means the operator expects exact continuation.
*/
func (chord *Chord) SetGuardRadius(radius uint8) {
	word := chord.C7()
	word &^= shellWordMaskGuardRadius
	word |= uint64(radius) << shellWordShiftGuardRadius
	chord.SetC7(word)
	chord.setOperatorFlag(ValueFlagGuard, true)
}

/*
GuardRadius retrieves the stored modular drift budget for the next hop.
*/
func (chord *Chord) GuardRadius() uint8 {
	return uint8((chord.C7() & shellWordMaskGuardRadius) >> shellWordShiftGuardRadius)
}

/*
HasGuard reports whether the value explicitly carries a transition guard.
*/
func (chord *Chord) HasGuard() bool {
	return chord.HasOperatorFlag(ValueFlagGuard)
}

/*
SetRouteHint stores a compact route class that biases the next hop toward
compatible continuation cells without redundantly storing a lexical byte.
*/
func (chord *Chord) SetRouteHint(route uint8) {
	word := chord.C7()
	word &^= shellWordMaskRouteHint
	word |= uint64(route) << shellWordShiftRouteHint
	chord.SetC7(word)
	chord.setOperatorFlag(ValueFlagRouteHint, true)
}

/*
RouteHint retrieves the shell-level continuation class carried by the value.
*/
func (chord *Chord) RouteHint() uint8 {
	return uint8((chord.C7() & shellWordMaskRouteHint) >> shellWordShiftRouteHint)
}

/*
HasRouteHint reports whether the value carries a route hint.
*/
func (chord *Chord) HasRouteHint() bool {
	return chord.HasOperatorFlag(ValueFlagRouteHint)
}

/*
SetLexicalTransition installs the native operator that would be induced by the
next byte under the original lexical phase rule. This preserves compatibility
with existing prompt/state logic while keeping the stored value lexical-free.

When the current state snapshot is already available in ResidualCarry, the value
also records a trajectory snapshot and a compact route hint in the shell so the
operator can steer traversal without replaying lexical identity from storage.
*/
func (chord *Chord) SetLexicalTransition(next byte) {
	chord.SetAffine(PhaseScaleForByte(next), 0)
	chord.SetRouteHint(RouteHintForSymbol(next))

	if carry := chord.ResidualCarry(); carry > 0 {
		current := numeric.Phase(carry % uint64(numeric.FermatPrime))
		if current > 0 {
			chord.SetTrajectory(current, chord.ApplyAffinePhase(current))
		}
	}
}
