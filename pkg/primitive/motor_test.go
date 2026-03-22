package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMod8191(t *testing.T) {
	Convey("Given mod8191", t, func() {
		Convey("It should pass through values below 8191", func() {
			So(mod8191(0), ShouldEqual, 0)
			So(mod8191(1), ShouldEqual, 1)
			So(mod8191(4000), ShouldEqual, 4000)
		})

		Convey("It should reduce 8191 to 0", func() {
			So(mod8191(8191), ShouldEqual, 0)
		})

		Convey("It should reduce multiples of 8191", func() {
			So(mod8191(8191*2), ShouldEqual, 0)
		})

		Convey("It should reduce large products correctly", func() {
			So(mod8191(8190*8190), ShouldEqual, (8190*8190)%8191)
		})
	})
}

func TestMotor(t *testing.T) {
	Convey("Given Values with different bit patterns", t, func() {
		Convey("An empty Value should derive identity motor", func() {
			v := NewValue()
			scale, translate := v.Motor()
			So(scale, ShouldEqual, 1)
			So(translate, ShouldEqual, 0)
		})

		Convey("A single bit should produce scale=index, translate=index", func() {
			v := NewValue()
			v.Set(3)

			scale, translate := v.Motor()
			So(scale, ShouldEqual, 3)
			So(translate, ShouldEqual, 3)
		})

		Convey("Different bit patterns should derive different motors", func() {
			a := NewValue()
			a.Set(3)
			a.Set(7)

			b := NewValue()
			b.Set(5)
			b.Set(11)

			scaleA, translateA := a.Motor()
			scaleB, translateB := b.Motor()

			So(scaleA != scaleB || translateA != translateB, ShouldBeTrue)
		})

		Convey("Dense words should hit the table-driven path and stay in GF(8191)", func() {
			v := NewValue()

			for i := 0; i < 8; i++ {
				v.Set(i)
			}

			scale, translate := v.Motor()
			So(scale, ShouldBeLessThan, CoreBits)
			So(translate, ShouldBeLessThan, CoreBits)
			So(scale, ShouldNotEqual, 0)
		})

		Convey("Many bits across words should stay within GF(8191)", func() {
			v := NewValue()

			for i := 0; i < 100; i++ {
				v.Set(i)
			}

			scale, translate := v.Motor()
			So(scale, ShouldBeLessThan, CoreBits)
			So(translate, ShouldBeLessThan, CoreBits)
		})

		Convey("Scale should never be zero (normalized to identity)", func() {
			v := NewValue()
			v.Set(0)

			scale, _ := v.Motor()
			So(scale, ShouldBeGreaterThanOrEqualTo, 1)
		})
	})
}

func TestMotorError(t *testing.T) {
	Convey("Given MotorError", t, func() {
		Convey("Error() should return the string representation", func() {
			So(ErrMotorNonInvertible.Error(), ShouldEqual, "motor: non-invertible scale")
		})
	})
}

func TestApplyMotor(t *testing.T) {
	Convey("Given ApplyMotor f(p) = scale*p + translate (mod 8191)", t, func() {
		Convey("Identity motor (1, 0) should be a no-op", func() {
			So(ApplyMotor(1, 0, 42), ShouldEqual, 42)
			So(ApplyMotor(1, 0, 0), ShouldEqual, 0)
			So(ApplyMotor(1, 0, 8190), ShouldEqual, 8190)
		})

		Convey("Pure translation (1, t) should add t mod 8191", func() {
			So(ApplyMotor(1, 10, 0), ShouldEqual, 10)
			So(ApplyMotor(1, 10, 8185), ShouldEqual, 4)
		})

		Convey("Pure scaling (s, 0) should multiply mod 8191", func() {
			So(ApplyMotor(5, 0, 100), ShouldEqual, 500)
			So(ApplyMotor(2, 0, 4096), ShouldEqual, (2*4096)%8191)
		})

		Convey("General case should compute (s*p + t) mod 8191", func() {
			So(ApplyMotor(5, 10, 42), ShouldEqual, (5*42+10)%8191)
			So(ApplyMotor(35, 12, 100), ShouldEqual, (35*100+12)%8191)
		})

		Convey("Boundary: position 0 returns translate", func() {
			So(ApplyMotor(999, 7, 0), ShouldEqual, 7)
		})

		Convey("Result wraps correctly at the field boundary", func() {
			So(ApplyMotor(1, 1, 8190), ShouldEqual, 0)
			So(ApplyMotor(8190, 1, 1), ShouldEqual, 0)
		})
	})
}

func TestComposeMotor(t *testing.T) {
	Convey("Given ComposeMotor f2(f1(p))", t, func() {
		Convey("Composing with identity (1,0) should be a no-op", func() {
			s, tr := ComposeMotor(5, 10, 1, 0)
			So(s, ShouldEqual, 5)
			So(tr, ShouldEqual, 10)
		})

		Convey("Identity composed with any motor returns that motor", func() {
			s, tr := ComposeMotor(1, 0, 7, 3)
			So(s, ShouldEqual, 7)
			So(tr, ShouldEqual, 3)
		})

		Convey("Known case: f1=(5,10), f2=(3,7) -> f_comp=(15, 37)", func() {
			s, tr := ComposeMotor(5, 10, 3, 7)
			So(s, ShouldEqual, 15)
			So(tr, ShouldEqual, 37)
		})

		Convey("Composition matches sequential application for all probe positions", func() {
			s1, t1 := uint16(35), uint16(12)
			s2, t2 := uint16(100), uint16(77)
			sC, tC := ComposeMotor(s1, t1, s2, t2)

			for _, pos := range []uint16{0, 1, 42, 1000, 4095, 8190} {
				sequential := ApplyMotor(s2, t2, ApplyMotor(s1, t1, pos))
				composed := ApplyMotor(sC, tC, pos)
				So(composed, ShouldEqual, sequential)
			}
		})

		Convey("Result components stay within GF(8191)", func() {
			s, tr := ComposeMotor(8190, 8190, 8190, 8190)
			So(s, ShouldBeLessThan, CoreBits)
			So(tr, ShouldBeLessThan, CoreBits)
		})
	})
}

func TestInvertMotor(t *testing.T) {
	Convey("Given InvertMotor", t, func() {
		Convey("Inverse of identity (1,0) is identity", func() {
			invS, invT, err := InvertMotor(1, 0)
			So(err, ShouldBeNil)
			So(invS, ShouldEqual, 1)
			So(invT, ShouldEqual, 0)
		})

		Convey("Inverse recovers the original position for all probes", func() {
			cases := []struct{ s, t uint16 }{
				{5, 10},
				{35, 12},
				{100, 77},
				{8190, 1},
				{7957, 2808},
			}

			for _, tc := range cases {
				invS, invT, err := InvertMotor(tc.s, tc.t)
				So(err, ShouldBeNil)

				for _, pos := range []uint16{0, 1, 42, 4095, 8190} {
					forward := ApplyMotor(tc.s, tc.t, pos)
					backward := ApplyMotor(invS, invT, forward)
					So(backward, ShouldEqual, pos)
				}
			}
		})

		Convey("Composing a motor with its inverse yields identity", func() {
			s, tr := uint16(35), uint16(12)
			invS, invT, err := InvertMotor(s, tr)
			So(err, ShouldBeNil)

			idS, idT := ComposeMotor(s, tr, invS, invT)
			So(idS, ShouldEqual, 1)
			So(idT, ShouldEqual, 0)
		})

		Convey("Scale 0 (before normalization) should return ErrMotorNonInvertible", func() {
			_, _, err := InvertMotor(0, 5)
			So(err, ShouldEqual, ErrMotorNonInvertible)
		})

		Convey("Non-invertible scale that shares factor with 8191 should error", func() {
			_, _, err := InvertMotor(8191, 0)
			So(err, ShouldEqual, ErrMotorNonInvertible)
		})
	})
}

func TestModInverse8191(t *testing.T) {
	Convey("Given modInverse8191", t, func() {
		Convey("Inverse of 1 is 1", func() {
			inv, err := modInverse8191(1)
			So(err, ShouldBeNil)
			So(inv, ShouldEqual, 1)
		})

		Convey("a * inverse(a) == 1 (mod 8191) for selected values", func() {
			probes := []uint16{2, 3, 5, 7, 35, 100, 1000, 8190}

			for _, a := range probes {
				inv, err := modInverse8191(a)
				So(err, ShouldBeNil)
				So((uint32(a)*uint32(inv))%uint32(CoreBits), ShouldEqual, 1)
			}
		})

		Convey("Inverse of 0 should fail", func() {
			_, err := modInverse8191(0)
			So(err, ShouldEqual, ErrMotorNonInvertible)
		})

		Convey("Inverse of 8191 should fail (congruent to 0)", func() {
			_, err := modInverse8191(8191)
			So(err, ShouldEqual, ErrMotorNonInvertible)
		})

		Convey("Known inverse: 35^-1 mod 8191 == 7957", func() {
			inv, err := modInverse8191(35)
			So(err, ShouldBeNil)
			So(inv, ShouldEqual, 7957)
		})
	})
}

func BenchmarkApplyMotor(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		ApplyMotor(35, 12, 4095)
	}
}

func BenchmarkComposeMotor(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		ComposeMotor(35, 12, 100, 77)
	}
}

func BenchmarkInvertMotor(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		InvertMotor(35, 12)
	}
}

func BenchmarkMotorEmpty(b *testing.B) {
	value := NewValue()
	b.ReportAllocs()

	for b.Loop() {
		value.Motor()
	}
}

func BenchmarkMotorSparse(b *testing.B) {
	value := NewValue()

	for i := 0; i < 50; i++ {
		value.Set(i * 163 % CoreBits)
	}

	b.ReportAllocs()

	for b.Loop() {
		value.Motor()
	}
}

func BenchmarkMotorDense(b *testing.B) {
	value := NewValue()

	for i := 0; i < 4000; i++ {
		value.Set(i * 2 % CoreBits)
	}

	b.ReportAllocs()

	for b.Loop() {
		value.Motor()
	}
}
