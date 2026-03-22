package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPrimeTable(t *testing.T) {
	Convey("Given the prime table", t, func() {
		Convey("It should start with the first primes in order", func() {
			So(Primes[0], ShouldEqual, 2)
			So(Primes[1], ShouldEqual, 3)
			So(Primes[2], ShouldEqual, 5)
			So(Primes[3], ShouldEqual, 7)
			So(Primes[4], ShouldEqual, 11)
			So(Primes[5], ShouldEqual, 13)
		})

		Convey("It should fill all CoreBits positions", func() {
			So(Primes[CoreBits-1], ShouldBeGreaterThan, 0)
		})

		Convey("Each entry should be strictly greater than the previous", func() {
			for i := 1; i < CoreBits; i++ {
				So(Primes[i], ShouldBeGreaterThan, Primes[i-1])
			}
		})
	})
}

func TestPrimeIndex(t *testing.T) {
	Convey("Given the reverse prime index", t, func() {
		Convey("It should map every table entry back to its position", func() {
			for i := 0; i < CoreBits; i++ {
				pos, ok := PrimeIndex[Primes[i]]
				So(ok, ShouldBeTrue)
				So(pos, ShouldEqual, i)
			}
		})
	})
}

func TestBaseValue(t *testing.T) {
	Convey("Given BaseValue", t, func() {
		Convey("Byte 0 should produce a zero Value", func() {
			v := BaseValue(0)
			So(v.IsZero(), ShouldBeTrue)
		})

		Convey("Byte 1 should produce a zero Value", func() {
			v := BaseValue(1)
			So(v.IsZero(), ShouldBeTrue)
		})

		Convey("Byte 2 should activate only position 0 (prime 2)", func() {
			v := BaseValue(2)
			So(v.PopCount(), ShouldEqual, 1)
			So(v.Has(0), ShouldBeTrue)
		})

		Convey("Byte 6 = 2×3 should activate positions {0, 1}", func() {
			v := BaseValue(6)
			So(v.PopCount(), ShouldEqual, 2)
			So(v.Has(0), ShouldBeTrue)
			So(v.Has(1), ShouldBeTrue)
		})

		Convey("Byte 30 = 2×3×5 should activate positions {0, 1, 2}", func() {
			v := BaseValue(30)
			So(v.PopCount(), ShouldEqual, 3)
			So(v.Has(0), ShouldBeTrue)
			So(v.Has(1), ShouldBeTrue)
			So(v.Has(2), ShouldBeTrue)
		})

		Convey("Byte 210 = 2×3×5×7 should activate positions {0, 1, 2, 3}", func() {
			v := BaseValue(210)
			So(v.PopCount(), ShouldEqual, 4)
			So(v.Has(0), ShouldBeTrue)
			So(v.Has(1), ShouldBeTrue)
			So(v.Has(2), ShouldBeTrue)
			So(v.Has(3), ShouldBeTrue)
		})

		Convey("Byte 65 = 5×13 should activate positions {2, 5}", func() {
			v := BaseValue(65)
			So(v.PopCount(), ShouldEqual, 2)
			So(v.Has(2), ShouldBeTrue)
			So(v.Has(5), ShouldBeTrue)
		})

		Convey("Byte 251 (prime) should activate exactly one bit", func() {
			v := BaseValue(251)
			So(v.PopCount(), ShouldEqual, 1)

			pos, ok := PrimeIndex[251]
			So(ok, ShouldBeTrue)
			So(v.Has(pos), ShouldBeTrue)
		})

		Convey("Byte 128 = 2^7 (square-free projection) should activate only position 0", func() {
			v := BaseValue(128)
			So(v.PopCount(), ShouldEqual, 1)
			So(v.Has(0), ShouldBeTrue)
		})

		Convey("AND of two BaseValues should give GCD factorization", func() {
			a := BaseValue(30)
			b := BaseValue(70)

			var gcd Value

			for i := range Words {
				gcd[i] = a[i] & b[i]
			}

			So(gcd.Has(0), ShouldBeTrue)
			So(gcd.Has(2), ShouldBeTrue)
			So(gcd.PopCount(), ShouldEqual, 2)
		})

		Convey("BaseValueInto should match BaseValue for every byte", func() {
			for byteValue := range 256 {
				var got Value

				BaseValueInto(&got, byte(byteValue))

				So(got.Equal(BaseValue(byte(byteValue))), ShouldBeTrue)
			}
		})

		Convey("BaseValueInto should overwrite the destination for zero bytes", func() {
			var got Value
			got.Set(123)

			BaseValueInto(&got, 0)

			So(got.IsZero(), ShouldBeTrue)
		})
	})
}

func BenchmarkBaseValue(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		BaseValue(210)
	}
}

func BenchmarkBaseValueInto(b *testing.B) {
	var value Value

	b.ReportAllocs()

	for b.Loop() {
		BaseValueInto(&value, 210)
	}
}

func BenchmarkPrimeIndexLookup(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_ = PrimeIndex[251]
	}
}
