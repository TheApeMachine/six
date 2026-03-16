package geometry

import (
	"math"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
)

func mockBaseValue(b byte) data.Value {
	_, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
	value, _ := data.NewValue(seg)
	totalBits := config.Numeric.ValueBlocks * 64

	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		bit := off % totalBits
		value.Set(bit)
	}

	return value
}

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
			var empty data.Value
			theta, phi := ei.PhaseForValue(&empty)
			So(theta, ShouldEqual, 0)
			So(phi, ShouldEqual, 0)
		})

		Convey("When computing phase for a mock base value", func() {
			value := mockBaseValue('A')
			theta, phi := ei.PhaseForValue(&value)

			// The mock value sets 5 bits, so density is 5.
			// Phi = 2 * π * 5 / 257 ≈ 0.122
			expectedPhi := 2.0 * math.Pi * 5.0 / 257.0

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
			values := []data.Value{
				mockBaseValue('A'),
				mockBaseValue('A'),
			}

			// Sequence of identical values should yield same phase as a single value
			singleTheta, singlePhi := ei.PhaseForValue(&values[0])
			seqTheta, seqPhi := ei.SeqToroidalMeanPhase(values)

			So(seqTheta, ShouldAlmostEqual, singleTheta, 0.001)
			So(seqPhi, ShouldAlmostEqual, singlePhi, 0.001)
		})

		Convey("When calling BuildMultiScaleCooccurrence", func() {
			values := []data.Value{mockBaseValue('a'), mockBaseValue('b')}
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
			values := []data.Value{mockBaseValue('X')}
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
			values := []data.Value{mockBaseValue('X')}
			anchor, _ := ei.WeightedCircularMean(values)

			// Same sequence should have distance 0 from its own anchor
			So(ei.IsGeometricallyClosed(values, anchor), ShouldBeTrue)
		})

		Convey("When sequence drifts to opposite side of Torus", func() {
			valuesA := []data.Value{mockBaseValue('X')}
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
	value := mockBaseValue('A')
	b.ResetTimer()
	for b.Loop() {
		ei.PhaseForValue(&value)
	}
}

func BenchmarkEigenModeSeqToroidalMeanPhase(b *testing.B) {
	ei := NewEigenMode()
	values := make([]data.Value, 64)
	for idx := range values {
		values[idx] = mockBaseValue(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.SeqToroidalMeanPhase(values)
	}
}

func BenchmarkEigenModeWeightedCircularMean(b *testing.B) {
	ei := NewEigenMode()
	values := make([]data.Value, 64)
	for idx := range values {
		values[idx] = mockBaseValue(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.WeightedCircularMean(values)
	}
}
