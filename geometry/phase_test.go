package geometry

import (
	"math"
	"math/cmplx"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
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

func TestPhaseDialEncode(t *testing.T) {
	Convey("Given an empty PhaseDial instance", t, func() {
		dial := NewPhaseDial()

		Convey("When encoding an empty string", func() {
			encoded := dial.Encode("")
			// Given nothing to rotate, it returns the zero array
			for i := range encoded {
				So(real(encoded[i]), ShouldEqual, 0)
				So(imag(encoded[i]), ShouldEqual, 0)
			}
		})

		Convey("When encoding extremely small structural tokens", func() {
			text := "a"
			encoded := dial.Encode(text)

			// Validate geometric fingerprint norm is roughly 1.0 (magnitude)
			var mag float64
			for _, val := range encoded {
				r, i := real(val), imag(val)
				mag += r*r + i*i
			}
			So(math.Sqrt(mag), ShouldAlmostEqual, 1.0, 0.0001)

			// Ensure non-zero mapping
			So(encoded[0], ShouldNotEqual, complex(0, 0))
		})

		Convey("When encoding longer structural sequences", func() {
			text1 := "hello world"
			text2 := "world hello" // Anagram to ensure order dictates phase angles

			encodedA := NewPhaseDial().Encode(text1)
			encodedB := NewPhaseDial().Encode(text2)

			// Normalization test
			var magA, magB float64
			for i := range encodedA {
				r, iPhase := real(encodedA[i]), imag(encodedA[i])
				magA += r*r + iPhase*iPhase

				rB, iPhaseB := real(encodedB[i]), imag(encodedB[i])
				magB += rB*rB + iPhaseB*iPhaseB
			}
			So(math.Sqrt(magA), ShouldAlmostEqual, 1.0, 0.0001)
			So(math.Sqrt(magB), ShouldAlmostEqual, 1.0, 0.0001)

			// Structural divergence test -> Different semantic order creates different holograms
			differences := 0
			for i := 0; i < len(encodedA); i++ {
				// Due to floating point math, we measure concrete differences directly
				dist := cmplx.Abs(encodedA[i] - encodedB[i])
				if dist > 0.001 {
					differences++
				}
			}
			// They should drastically diverge over the NBasis space
			So(differences, ShouldBeGreaterThan, 100)
		})
	})
}
