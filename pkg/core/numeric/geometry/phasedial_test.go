package geometry

import (
	"math"
	"math/cmplx"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestNewPhaseDial(t *testing.T) {
	Convey("Given NewPhaseDial", t, func() {
		Convey("It should return a zeroed dial of PhaseDialDimensions length", func() {
			dial := NewPhaseDial()
			So(dial, ShouldNotBeNil)
			So(len(dial), ShouldEqual, PhaseDialDimensions)

			for _, val := range dial {
				So(real(val), ShouldEqual, 0)
				So(imag(val), ShouldEqual, 0)
			}
		})
	})
}

func TestPhaseDialEncodeFromValues(t *testing.T) {
	Convey("Given PhaseDial encoding", t, func() {
		dial := NewPhaseDial()

		Convey("When encoding an empty sequence", func() {
			encoded := dial.EncodeFromValues(nil)
			So(encoded, ShouldNotBeNil)
		})

		Convey("When encoding a single value", func() {
			values, err := primitive.NewValue([]byte("a"))
			So(err, ShouldBeNil)
			So(len(values), ShouldEqual, 1)

			encoded := NewPhaseDial().EncodeFromValues([]primitive.Value{*values[0]})
			var mag float64

			for _, val := range encoded {
				re, im := real(val), imag(val)
				mag += re*re + im*im
			}

			So(math.Sqrt(mag), ShouldAlmostEqual, 1.0, 0.0001)
			So(encoded[0], ShouldNotEqual, complex(0, 0))
		})
	})
}

func TestPhaseDialSimilarity(t *testing.T) {
	Convey("Given distinct payloads", t, func() {
		seqA := make([]byte, 50)

		for i := range seqA {
			seqA[i] = 10
		}

		seqB := make([]byte, 50)

		for i := range seqB {
			seqB[i] = 200
		}

		va, errA := primitive.NewValue(seqA)
		vb, errB := primitive.NewValue(seqB)
		So(errA, ShouldBeNil)
		So(errB, ShouldBeNil)

		encodedA := NewPhaseDial().EncodeFromValues([]primitive.Value{*va[0]})
		encodedB := NewPhaseDial().EncodeFromValues([]primitive.Value{*vb[0]})

		differences := 0

		for i := range encodedA {
			if cmplx.Abs(encodedA[i]-encodedB[i]) > 0.001 {
				differences++
			}
		}

		So(differences, ShouldBeGreaterThan, 100)

		sim := encodedA.Similarity(encodedB)
		So(sim, ShouldBeBetweenOrEqual, -1, 1)
		So(sim, ShouldNotAlmostEqual, 1.0, 0.01)
	})
}

func TestNewPhaseRotor(t *testing.T) {
	Convey("Given NewPhaseRotor", t, func() {
		r := NewPhaseRotor()
		So(len(r), ShouldEqual, PhaseDialDimensions)

		for _, mv := range r {
			for _, component := range mv {
				So(component, ShouldEqual, 0)
			}
		}

		values, err := primitive.NewValue([]byte("rotor"))
		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		encoded := NewPhaseRotor().EncodeFromValues([]primitive.Value{*values[0]})
		So(len(encoded), ShouldEqual, PhaseDialDimensions)

		selfSim := encoded.Similarity(encoded)
		So(selfSim, ShouldAlmostEqual, 1.0, 0.0001)

		dial := encoded.ToDialCompat()
		So(len(dial), ShouldEqual, PhaseDialDimensions)

		var mag float64

		for _, val := range dial {
			re, im := real(val), imag(val)
			mag += re*re + im*im
		}

		So(math.Sqrt(mag), ShouldAlmostEqual, 1.0, 0.0001)
	})
}

func BenchmarkNewPhaseDial(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewPhaseDial()
	}
}

func BenchmarkPhaseDialEncodeFromValues(b *testing.B) {
	values, err := primitive.NewValue([]byte("benchmark value sequence for phase encoding"))
	if err != nil || len(values) < 1 {
		b.Fatal(err)
	}

	dial := NewPhaseDial()
	payload := []primitive.Value{*values[0]}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = dial.EncodeFromValues(payload)
	}
}
