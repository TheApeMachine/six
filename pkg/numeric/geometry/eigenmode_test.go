package geometry

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

func TestNewEigenMode(t *testing.T) {
	Convey("Given NewEigenMode constructor", t, func() {
		Convey("When creating with no options", func() {
			ei := NewEigenMode()
			Convey("It should correctly initialize as trained", func() {
				So(ei, ShouldNotBeNil)
				So(ei.Trained, ShouldBeTrue) // Analytical mode is always trained
			})
		})

		Convey("When creating with options", func() {
			opt := func(ei *EigenMode) {
				ei.Trained = false // dummy test
			}
			ei := NewEigenMode(opt)
			Convey("It should apply the provided options", func() {
				So(ei.Trained, ShouldBeFalse)
			})
		})
	})
}

func TestAnalyticalPhaseGeneration(t *testing.T) {
	Convey("Given a value-native EigenMode", t, func() {
		ei := NewEigenMode()

		Convey("When computing phase for an empty value", func() {
			var empty primitive.Value
			theta, phi := ei.PhaseForValue(&empty)
			So(theta, ShouldEqual, 0)
			So(phi, ShouldEqual, 0)
		})

		Convey("When computing phase for a mock base value", func() {
			value := primitive.BaseValue('A')
			theta, phi := ei.PhaseForValue(&value)

			expectedPhi := 2.0 * math.Pi * float64(value.ActiveCount()) / 257.0

			So(phi, ShouldAlmostEqual, expectedPhi, 0.001)

			// Theta should be within [-π, π]
			So(theta, ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
		})

		Convey("When computing mean sequence phase for empty values", func() {
			theta, phi := ei.SeqToroidalMeanPhase(nil)
			So(theta, ShouldEqual, 0)
			So(phi, ShouldEqual, 0)
		})

		Convey("When computing mean sequence phase", func() {
			values := []primitive.Value{
				primitive.BaseValue('A'),
				primitive.BaseValue('A'),
			}

			// Sequence of identical values should yield same phase as a single value
			singleTheta, singlePhi := ei.PhaseForValue(&values[0])
			seqTheta, seqPhi := ei.SeqToroidalMeanPhase(values)

			So(seqTheta, ShouldAlmostEqual, singleTheta, 0.001)
			So(seqPhi, ShouldAlmostEqual, singlePhi, 0.001)
		})

		Convey("When calling BuildMultiScaleCooccurrence", func() {
			values := []primitive.Value{primitive.BaseValue('a'), primitive.BaseValue('b')}
			err := ei.BuildMultiScaleCooccurrence(values)
			So(err, ShouldBeNil)
			So(ei.Trained, ShouldBeTrue)
		})
	})
}

func TestEigenModeWeightedCircularMean(t *testing.T) {
	Convey("Given WeightedCircularMean", t, func() {
		ei := NewEigenMode()

		Convey("When values slice is empty", func() {
			phase, conc := ei.WeightedCircularMean(nil)
			So(phase, ShouldEqual, 0)
			So(conc, ShouldEqual, 0)
		})

		Convey("When computing for single value", func() {
			values := []primitive.Value{primitive.BaseValue('X')}
			phase, conc := ei.WeightedCircularMean(values)
			So(phase, ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
			So(conc, ShouldBeBetweenOrEqual, 0, 1)
		})
	})
}

func TestGeometricalClosure(t *testing.T) {
	Convey("Given IsGeometricallyClosed", t, func() {
		ei := NewEigenMode()

		Convey("When sequence is empty", func() {
			So(ei.IsGeometricallyClosed(nil, 0), ShouldBeFalse)
		})

		Convey("When sequence returns exactly to anchor phase", func() {
			values := []primitive.Value{primitive.BaseValue('X')}
			anchor, _ := ei.WeightedCircularMean(values)

			// Same sequence should have distance 0 from its own anchor
			So(ei.IsGeometricallyClosed(values, anchor), ShouldBeTrue)
		})

		Convey("When sequence drifts to opposite side of Torus", func() {
			valuesA := []primitive.Value{primitive.BaseValue('X')}
			anchor, _ := ei.WeightedCircularMean(valuesA)

			// We manually specify an anchor that is π radians away
			oppositeAnchor := anchor + math.Pi
			if oppositeAnchor > math.Pi {
				oppositeAnchor -= 2 * math.Pi
			}

			So(ei.IsGeometricallyClosed(valuesA, oppositeAnchor), ShouldBeFalse)
		})
	})
}

func BenchmarkEigenModePhaseForValue(b *testing.B) {
	ei := NewEigenMode()
	value := primitive.BaseValue('A')
	b.ResetTimer()
	for b.Loop() {
		ei.PhaseForValue(&value)
	}
}

func BenchmarkEigenModeSeqToroidalMeanPhase(b *testing.B) {
	ei := NewEigenMode()
	values := make([]primitive.Value, 64)
	for idx := range values {
		values[idx] = primitive.BaseValue(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.SeqToroidalMeanPhase(values)
	}
}

func BenchmarkEigenModeWeightedCircularMean(b *testing.B) {
	ei := NewEigenMode()
	values := make([]primitive.Value, 64)
	for idx := range values {
		values[idx] = primitive.BaseValue(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.WeightedCircularMean(values)
	}
}
