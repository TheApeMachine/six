package geometry

import (
	"math"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/data"
)

func mockBaseChord(b byte) data.Chord {
	_, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
	chord, _ := data.NewChord(seg)
	totalBits := config.Numeric.ChordBlocks * 64

	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		bit := off % totalBits
		chord.Set(bit)
	}

	return chord
}

func TestNewEigenMode(t *testing.T) {
	Convey("Given NewEigenMode constructor", t, func() {
		Convey("When creating with no options", func() {
			ei := NewEigenMode()
			So(ei, ShouldNotBeNil)
			So(ei.Trained, ShouldBeTrue) // Analytical mode is always trained
		})

		Convey("When creating with options", func() {
			opt := func(ei *EigenMode) {
				ei.Trained = false // dummy test
			}
			ei := NewEigenMode(opt)
			So(ei.Trained, ShouldBeFalse)
		})
	})
}

func TestAnalyticalPhaseGeneration(t *testing.T) {
	Convey("Given a chord-native EigenMode", t, func() {
		ei := NewEigenMode()

		Convey("When computing phase for an empty chord", func() {
			var empty data.Chord
			theta, phi := ei.PhaseForChord(&empty)
			So(theta, ShouldEqual, 0)
			So(phi, ShouldEqual, 0)
		})

		Convey("When computing phase for a mock base chord", func() {
			chord := mockBaseChord('A')
			theta, phi := ei.PhaseForChord(&chord)

			// The mock chord sets 5 bits, so density is 5.
			// Phi = 2 * π * 5 / 257 ≈ 0.122
			expectedPhi := 2.0 * math.Pi * 5.0 / 257.0

			So(phi, ShouldAlmostEqual, expectedPhi, 0.001)

			// Theta should be within [-π, π]
			So(theta, ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
		})

		Convey("When computing mean sequence phase for empty chords", func() {
			theta, phi := ei.SeqToroidalMeanPhase(nil)
			So(theta, ShouldEqual, 0)
			So(phi, ShouldEqual, 0)
		})

		Convey("When computing mean sequence phase", func() {
			chords := []data.Chord{
				mockBaseChord('A'),
				mockBaseChord('A'),
			}

			// Sequence of identical chords should yield same phase as a single chord
			singleTheta, singlePhi := ei.PhaseForChord(&chords[0])
			seqTheta, seqPhi := ei.SeqToroidalMeanPhase(chords)

			So(seqTheta, ShouldAlmostEqual, singleTheta, 0.001)
			So(seqPhi, ShouldAlmostEqual, singlePhi, 0.001)
		})

		Convey("When calling BuildMultiScaleCooccurrence", func() {
			chords := []data.Chord{mockBaseChord('a'), mockBaseChord('b')}
			err := ei.BuildMultiScaleCooccurrence(chords)
			So(err, ShouldBeNil)
			So(ei.Trained, ShouldBeTrue)
		})
	})
}

func TestEigenModeWeightedCircularMean(t *testing.T) {
	Convey("Given WeightedCircularMean", t, func() {
		ei := NewEigenMode()

		Convey("When chords slice is empty", func() {
			phase, conc := ei.WeightedCircularMean(nil)
			So(phase, ShouldEqual, 0)
			So(conc, ShouldEqual, 0)
		})

		Convey("When computing for single chord", func() {
			chords := []data.Chord{mockBaseChord('X')}
			phase, conc := ei.WeightedCircularMean(chords)
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
			chords := []data.Chord{mockBaseChord('X')}
			anchor, _ := ei.WeightedCircularMean(chords)

			// Same sequence should have distance 0 from its own anchor
			So(ei.IsGeometricallyClosed(chords, anchor), ShouldBeTrue)
		})

		Convey("When sequence drifts to opposite side of Torus", func() {
			chordsA := []data.Chord{mockBaseChord('X')}
			anchor, _ := ei.WeightedCircularMean(chordsA)

			// We manually specify an anchor that is π radians away
			oppositeAnchor := anchor + math.Pi
			if oppositeAnchor > math.Pi {
				oppositeAnchor -= 2 * math.Pi
			}

			So(ei.IsGeometricallyClosed(chordsA, oppositeAnchor), ShouldBeFalse)
		})
	})
}

func BenchmarkEigenModePhaseForChord(b *testing.B) {
	ei := NewEigenMode()
	chord := mockBaseChord('A')
	b.ResetTimer()
	for b.Loop() {
		ei.PhaseForChord(&chord)
	}
}

func BenchmarkEigenModeSeqToroidalMeanPhase(b *testing.B) {
	ei := NewEigenMode()
	chords := make([]data.Chord, 64)
	for idx := range chords {
		chords[idx] = mockBaseChord(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.SeqToroidalMeanPhase(chords)
	}
}

func BenchmarkEigenModeWeightedCircularMean(b *testing.B) {
	ei := NewEigenMode()
	chords := make([]data.Chord, 64)
	for idx := range chords {
		chords[idx] = mockBaseChord(byte(idx % 256))
	}
	b.ResetTimer()
	for b.Loop() {
		ei.WeightedCircularMean(chords)
	}
}
