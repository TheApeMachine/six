package numeric

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCalculus(t *testing.T) {
	Convey("Given a new Calculus engine for GF(257)", t, func() {
		calc := NewCalculus()

		Convey("It should hash strings into identical phases", func() {
			s1 := calc.Sum("Roy")
			s2 := calc.Sum("Roy")
			s3 := calc.Sum("Sandra")
			So(s1, ShouldEqual, s2)
			So(s1, ShouldNotEqual, s3)
		})

		Convey("It should support GF(257) multiplication and addition operations", func() {
			p1 := calc.Sum("Sandra")
			p2 := calc.Sum("Garden")

			mult := calc.Multiply(p1, p2)
			add := calc.Add(p1, p2)

			So(mult, ShouldBeLessThan, FermatPrime)
			So(add, ShouldBeLessThan, FermatPrime)

			sub := calc.Subtract(add, p2)
			So(sub, ShouldEqual, p1)
		})

		Convey("It should compute modular inverses that cancel exactly", func() {
			s := calc.Sum("Roy")
			l := calc.Sum("is_in")
			o := calc.Sum("Kitchen")

			// Combine into a single resonance phase
			combined := calc.Multiply(calc.Multiply(s, l), o)

			// Get the inverse of Roy and is_in
			invS := calc.Inverse(s)
			invL := calc.Inverse(l)

			// (s * l * o) * invS * invL = o
			resolved := calc.Multiply(calc.Multiply(combined, invS), invL)

			So(resolved, ShouldEqual, o)
		})

		Convey("It should correctly cancel negative constraints", func() {
			path := Phase(50)
			antiPath := calc.Subtract(Phase(FermatPrime), path) // This is also FermatPrime - Path

			// Combine them should yield 0 (Destructive interference)
			res := calc.Add(path, antiPath)
			So(res, ShouldEqual, 0)
		})
	})
}
