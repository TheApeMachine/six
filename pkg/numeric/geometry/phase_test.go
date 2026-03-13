package geometry

import (
	"math"
	"math/cmplx"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
)

func TestNewPhaseDial(t *testing.T) {
	Convey("Given the NewPhaseDial constructor", t, func() {
		Convey("When creating a new PhaseDial", func() {
			dial := NewPhaseDial()
			So(dial, ShouldNotBeNil)
			// Ensure it scales correctly via the shared underlying NBasis primitives
			So(len(dial), ShouldEqual, config.Numeric.NBasis)
			for _, val := range dial {
				So(real(val), ShouldEqual, 0)
				So(imag(val), ShouldEqual, 0)
			}
		})
	})
}

func TestPhaseDialEncodeFromChords(t *testing.T) {
	Convey("Given a PhaseDial and chord sequences", t, func() {
		dial := NewPhaseDial()

		Convey("When encoding an empty chord sequence", func() {
			encoded := dial.EncodeFromChords(nil)
			So(encoded, ShouldNotBeNil)
			for idx := range encoded {
				So(real(encoded[idx]), ShouldEqual, 0)
				So(imag(encoded[idx]), ShouldEqual, 0)
			}
		})

		Convey("When encoding a single chord", func() {
			chords := []data.Chord{data.BaseChord('a')}
			encoded := dial.EncodeFromChords(chords)
			var mag float64
			for _, val := range encoded {
				re, im := real(val), imag(val)
				mag += re*re + im*im
			}
			So(math.Sqrt(mag), ShouldAlmostEqual, 1.0, 0.0001)
			So(encoded[0], ShouldNotEqual, complex(0, 0))
		})

		Convey("When encoding different chord orderings", func() {
			sequenceA := []byte{}
			sequenceB := []byte{}
			for i := 0; i < 50; i++ {
				sequenceA = append(sequenceA, byte(10))
				sequenceB = append(sequenceB, byte(200))
			}
			chordsA, _ := data.BuildChord(sequenceA)
			chordsB, _ := data.BuildChord(sequenceB)
			encodedA := NewPhaseDial().EncodeFromChords([]data.Chord{chordsA})
			encodedB := NewPhaseDial().EncodeFromChords([]data.Chord{chordsB})

			// Normalization: both should be unit magnitude
			var magA, magB float64
			for i := range encodedA {
				r, im := real(encodedA[i]), imag(encodedA[i])
				magA += r*r + im*im
				rB, imB := real(encodedB[i]), imag(encodedB[i])
				magB += rB*rB + imB*imB
			}
			So(math.Sqrt(magA), ShouldAlmostEqual, 1.0, 0.0001)
			So(math.Sqrt(magB), ShouldAlmostEqual, 1.0, 0.0001)

			// Structural divergence: different chords create different holograms
			differences := 0
			for i := 0; i < len(encodedA); i++ {
				if cmplx.Abs(encodedA[i]-encodedB[i]) > 0.001 {
					differences++
				}
			}
			So(differences, ShouldBeGreaterThan, 100)

			// Similarity should be bounded and not nearly identical
			sim := encodedA.Similarity(encodedB)
			So(sim, ShouldBeBetweenOrEqual, -1, 1)
			So(sim, ShouldNotAlmostEqual, 1.0, 0.01)
		})
	})
}

func BenchmarkNewPhaseDial(b *testing.B) {
	for b.Loop() {
		_ = NewPhaseDial()
	}
}

func BenchmarkPhaseDialEncodeFromChords(b *testing.B) {
	dial := NewPhaseDial()
	chords, _ := data.BuildChord([]byte("benchmark chord sequence for phase encoding"))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = dial.EncodeFromChords([]data.Chord{chords})
	}
}
