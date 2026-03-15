package data

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestNeutralValueProjectionAndAffineOperator(t *testing.T) {
	gc.Convey("Given a lexical-free native value", t, func() {
		symbol := byte('Q')
		phase := numeric.Phase(19)
		next := byte('Z')

		value := NeutralValue()
		value.SetStatePhase(phase)
		value.SetLexicalTransition(next)
		value.SetProgram(OpcodeNext, 1, 0, false)

		gc.Convey("it should stay lexical-free until projected", func() {
			gc.So(HasLexicalSeed(value, symbol), gc.ShouldBeFalse)

			projected := SeedObservable(symbol, value)
			gc.So(HasLexicalSeed(projected, symbol), gc.ShouldBeTrue)

			stored := StorageValue(symbol, projected)
			gc.So(HasLexicalSeed(stored, symbol), gc.ShouldBeFalse)
			gc.So(stored.ResidualCarry(), gc.ShouldEqual, uint64(phase))
		})

		gc.Convey("its shell operator should reproduce the lexical phase transition", func() {
			calc := numeric.NewCalculus()
			expected := calc.Multiply(phase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(next)))
			gc.So(value.ApplyAffinePhase(phase), gc.ShouldEqual, expected)
		})
	})
}

func TestAffineTranslationAppliesInGF257(t *testing.T) {
	gc.Convey("Given an explicit affine shell operator", t, func() {
		value := NeutralValue()
		value.SetAffine(7, 5)

		gc.Convey("it should apply translation as well as scale", func() {
			gc.So(value.ApplyAffinePhase(11), gc.ShouldEqual, numeric.Phase((7*11+5)%int(numeric.FermatPrime)))
		})
	})
}
