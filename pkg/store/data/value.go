package data

import "github.com/theapemachine/six/pkg/numeric"

const (
	affineFieldMask          = uint64(0x1FF)
	affineWordShiftScale     = 0
	affineWordShiftTranslate = 9
	affineWordMaskScale      = affineFieldMask << affineWordShiftScale
	affineWordMaskTranslate  = affineFieldMask << affineWordShiftTranslate
)

var bytePhaseScales = initBytePhaseScales()

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
	s := uint64(uint32(scale) % numeric.FermatPrime)
	if s == 0 {
		s = 1
	}
	t := uint64(uint32(translate) % numeric.FermatPrime)

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
HasAffine reports whether the value explicitly carries a shell operator.
Legacy values return false and should fall back to lexical transition logic.
*/
func (chord *Chord) HasAffine() bool {
	return chord.C7() != 0
}

/*
ApplyAffinePhase advances a phase through the value's local affine operator.
*/
func (chord *Chord) ApplyAffinePhase(phase numeric.Phase) numeric.Phase {
	scale, translate := chord.Affine()
	return numeric.Phase((uint32(scale)*uint32(phase) + uint32(translate)) % numeric.FermatPrime)
}

/*
SetLexicalTransition installs the native operator that would be induced by the
next byte under the original lexical phase rule. This preserves compatibility
with existing prompt/state logic while keeping the stored value lexical-free.
*/
func (chord *Chord) SetLexicalTransition(next byte) {
	chord.SetAffine(PhaseScaleForByte(next), 0)
}
