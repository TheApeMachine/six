package integration

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
valuesFromSentence projects each byte of text into a Value sequence.
*/
func valuesFromSentence(sentence string) []*primitive.Value {
	data := []byte(sentence)
	values := make([]*primitive.Value, 0, len(data))

	for _, b := range data {
		values = append(values, primitive.BaseValue(b))
	}

	return values
}

/*
equalValueSequence checks exact Value equality at each position.
*/
func equalValueSequence(left, right []*primitive.Value) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if !left[index].Equal(right[index]) {
			return false
		}
	}

	return true
}

func TestMotorIsDeterministicForSameBitPattern(t *testing.T) {
	gc.Convey("Given the same Value bit pattern", t, func() {
		value := primitive.BaseValue(210)
		scaleA, translateA := value.Motor()
		scaleB, translateB := value.Motor()

		gc.So(scaleA, gc.ShouldEqual, scaleB)
		gc.So(translateA, gc.ShouldEqual, translateB)

		buffer := make([]byte, primitive.ByteSize)
		_, readErr := value.Read(buffer)
		gc.So(readErr, gc.ShouldEqual, io.EOF)

		clone := primitive.NewValue()
		_, writeErr := clone.Write(buffer)
		gc.So(writeErr, gc.ShouldBeNil)

		scaleClone, translateClone := clone.Motor()
		gc.So(scaleA, gc.ShouldEqual, scaleClone)
		gc.So(translateA, gc.ShouldEqual, translateClone)
	})
}

func TestMotorChangesAcrossDistinctBitPatterns(t *testing.T) {
	gc.Convey("Given two different Values", t, func() {
		left := primitive.BaseValue(2)
		right := primitive.BaseValue(3)

		scaleLeft, translateLeft := left.Motor()
		scaleRight, translateRight := right.Motor()

		gc.So(scaleLeft == scaleRight && translateLeft == translateRight, gc.ShouldBeFalse)
	})
}

func TestMotorNormalizesNonInvertibleScale(t *testing.T) {
	gc.Convey("Given a Value with only prime-index zero active", t, func() {
		value := primitive.BaseValue(2)
		scale, translate := value.Motor()

		gc.So(scale, gc.ShouldEqual, 1)
		gc.So(translate, gc.ShouldEqual, 0)
	})
}

func TestMotorCompositionMatchesAffineLaw(t *testing.T) {
	gc.Convey("Given two Values with derived motors", t, func() {
		first := primitive.BaseValue(30)
		second := primitive.BaseValue(70)
		scale1, translate1 := first.Motor()
		scale2, translate2 := second.Motor()
		composedScale, composedTranslate := primitive.ComposeMotor(scale1, translate1, scale2, translate2)

		positions := []uint16{0, 1, 42, 1024, 4096, 8190}

		for _, position := range positions {
			sequential := primitive.ApplyMotor(
				scale2,
				translate2,
				primitive.ApplyMotor(scale1, translate1, position),
			)
			composed := primitive.ApplyMotor(composedScale, composedTranslate, position)

			gc.So(
				sequential,
				gc.ShouldEqual,
				composed,
			)
		}
	})
}

func TestMotorInverseRecoversOriginalPosition(t *testing.T) {
	gc.Convey("Given a Value-derived motor in GF(8191)", t, func() {
		value := primitive.BaseValue(255)
		scale, translate := value.Motor()
		inverseScale, inverseTranslate, inverseErr := primitive.InvertMotor(scale, translate)
		gc.So(inverseErr, gc.ShouldBeNil)
		gc.So(inverseScale, gc.ShouldNotEqual, 0)

		positions := []uint16{0, 1, 17, 2048, 4095, 8190}

		for _, position := range positions {
			forward := primitive.ApplyMotor(scale, translate, position)
			backward := primitive.ApplyMotor(inverseScale, inverseTranslate, forward)

			gc.So(backward, gc.ShouldEqual, position)
		}
	})
}

func TestInBandMotorApplyThroughTransport(t *testing.T) {
	gc.Convey("Given in-band motor apply instruction through transport pipeline", t, func() {
		instruction := primitive.NewValue()
		instruction.SetInstruction(primitive.InstrMotorApply)

		motorSource := primitive.BaseValue(30)
		payload := primitive.BaseValue(210)

		out, err := runInBandOp(instruction, motorSource, payload)
		gc.So(err, gc.ShouldBeNil)

		scale, translate := motorSource.Motor()

		for bit := 0; bit < primitive.CoreBits; bit++ {
			if !payload.Has(bit) {
				continue
			}

			mapped := primitive.ApplyMotor(scale, translate, uint16(bit))
			gc.So(out.Has(int(mapped)), gc.ShouldBeTrue)
		}

		gc.So(out.PopCount(), gc.ShouldEqual, payload.PopCount())
	})
}

func TestInBandMotorInverseThroughTransport(t *testing.T) {
	gc.Convey("Given in-band motor apply and inverse instructions", t, func() {
		applyInstruction := primitive.NewValue()
		applyInstruction.SetInstruction(primitive.InstrMotorApply)

		invertInstruction := primitive.NewValue()
		invertInstruction.SetInstruction(primitive.InstrMotorInvert)

		motorSource := primitive.BaseValue(30)
		original := primitive.BaseValue(210)

		forward, forwardErr := runInBandOp(applyInstruction, motorSource, original)
		gc.So(forwardErr, gc.ShouldBeNil)

		backward, backwardErr := runInBandOp(invertInstruction, motorSource, forward)
		gc.So(backwardErr, gc.ShouldBeNil)
		gc.So(backward.Equal(original), gc.ShouldBeTrue)
	})
}

func TestInBandMotorComposeThroughTransport(t *testing.T) {
	gc.Convey("Given in-band motor compose instruction through transport pipeline", t, func() {
		instruction := primitive.NewValue()
		instruction.SetInstruction(primitive.InstrMotorCompose)

		left := primitive.BaseValue(30)
		right := primitive.BaseValue(210)

		scaleA, translateA := left.Motor()
		scaleB, translateB := right.Motor()
		composedScale, composedTranslate := primitive.ComposeMotor(
			scaleA, translateA, scaleB, translateB,
		)

		out, err := runInBandOp(instruction, left, right)
		gc.So(err, gc.ShouldBeNil)

		for bit := 0; bit < primitive.CoreBits; bit++ {
			if !right.Has(bit) {
				continue
			}

			mapped := primitive.ApplyMotor(composedScale, composedTranslate, uint16(bit))
			gc.So(out.Has(int(mapped)), gc.ShouldBeTrue)
		}

		gc.So(out.PopCount(), gc.ShouldEqual, right.PopCount())
	})
}

func TestInBandMotorSequenceRoundTripForwardBackward(t *testing.T) {
	gc.Convey("Given a full sequence of in-band motor steps", t, func() {
		applyInstruction := primitive.NewValue()
		applyInstruction.SetInstruction(primitive.InstrMotorApply)

		invertInstruction := primitive.NewValue()
		invertInstruction.SetInstruction(primitive.InstrMotorInvert)

		sources := []*primitive.Value{
			primitive.BaseValue(30),
			primitive.BaseValue(70),
			primitive.BaseValue(255),
			primitive.BaseValue(105),
		}
		original := primitive.BaseValue(210)
		current := original

		for _, source := range sources {
			next, nextErr := runInBandOp(applyInstruction, source, current)
			gc.So(nextErr, gc.ShouldBeNil)
			current = next
		}

		for index := len(sources) - 1; index >= 0; index-- {
			previous, previousErr := runInBandOp(invertInstruction, sources[index], current)
			gc.So(previousErr, gc.ShouldBeNil)
			current = previous
		}

		gc.So(current.Equal(original), gc.ShouldBeTrue)
	})
}

func TestInBandMotorSequenceDeterminism(t *testing.T) {
	gc.Convey("Given the same source sequence and initial Value", t, func() {
		applyInstruction := primitive.NewValue()
		applyInstruction.SetInstruction(primitive.InstrMotorApply)

		sources := []*primitive.Value{
			primitive.BaseValue(30),
			primitive.BaseValue(70),
			primitive.BaseValue(255),
		}
		runSequence := func(start *primitive.Value) (*primitive.Value, error) {
			current := start

			for _, source := range sources {
				next, nextErr := runInBandOp(applyInstruction, source, current)
				if nextErr != nil {
					return nil, nextErr
				}

				current = next
			}

			return current, nil
		}

		start := primitive.BaseValue(210)
		first, firstErr := runSequence(start)
		gc.So(firstErr, gc.ShouldBeNil)

		second, secondErr := runSequence(start)
		gc.So(secondErr, gc.ShouldBeNil)
		gc.So(second.Equal(first), gc.ShouldBeTrue)
	})
}

func TestInBandSentenceRoundTripEqualsOriginalSequence(t *testing.T) {
	gc.Convey("Given a transformed sentence sequence through in-band motor navigation", t, func() {
		applyInstruction := primitive.NewValue()
		applyInstruction.SetInstruction(primitive.InstrMotorApply)

		invertInstruction := primitive.NewValue()
		invertInstruction.SetInstruction(primitive.InstrMotorInvert)

		original := valuesFromSentence("the cat sat on the mat")
		encoded := make([]*primitive.Value, len(original))

		encoded[0] = original[0]

		for index := 1; index < len(original); index++ {
			next, nextErr := runInBandOp(applyInstruction, encoded[index-1], original[index])
			gc.So(nextErr, gc.ShouldBeNil)
			encoded[index] = next
		}

		decoded := make([]*primitive.Value, len(original))
		decoded[0] = encoded[0]

		for index := 1; index < len(encoded); index++ {
			previous, previousErr := runInBandOp(invertInstruction, encoded[index-1], encoded[index])
			gc.So(previousErr, gc.ShouldBeNil)
			decoded[index] = previous
		}

		gc.So(equalValueSequence(decoded, original), gc.ShouldBeTrue)
	})
}

func TestInBandSentenceRoundTripTable(t *testing.T) {
	gc.Convey("Given multiple sentence fixtures with spaces and punctuation", t, func() {
		applyInstruction := primitive.NewValue()
		applyInstruction.SetInstruction(primitive.InstrMotorApply)

		invertInstruction := primitive.NewValue()
		invertInstruction.SetInstruction(primitive.InstrMotorInvert)

		sentences := []string{
			"the cat sat on the mat",
			"weather shifts, but primes persist.",
			"sort, write, close",
			"A Value is both data and operation.",
		}

		for _, sentence := range sentences {
			sentence := sentence

			gc.Convey(sentence, func() {
				original := valuesFromSentence(sentence)
				encoded := make([]*primitive.Value, len(original))
				decoded := make([]*primitive.Value, len(original))

				encoded[0] = original[0]
				decoded[0] = encoded[0]

				for index := 1; index < len(original); index++ {
					next, nextErr := runInBandOp(
						applyInstruction,
						encoded[index-1],
						original[index],
					)
					gc.So(nextErr, gc.ShouldBeNil)
					encoded[index] = next
				}

				for index := 1; index < len(encoded); index++ {
					previous, previousErr := runInBandOp(
						invertInstruction,
						encoded[index-1],
						encoded[index],
					)
					gc.So(previousErr, gc.ShouldBeNil)
					decoded[index] = previous
				}

				gc.So(equalValueSequence(decoded, original), gc.ShouldBeTrue)
			})
		}
	})
}

var motorBenchS, motorBenchT uint16

func BenchmarkMotorDerivationFromValue(b *testing.B) {
	value := primitive.BaseValue(255)
	b.ReportAllocs()

	for b.Loop() {
		motorBenchS, motorBenchT = value.Motor()
	}
}
