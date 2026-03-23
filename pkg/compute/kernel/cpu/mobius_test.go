package cpu

import (
	"math/bits"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestMobiusSign(t *testing.T) {
	gc.Convey("Given Values with known popcount parity", t, func() {
		gc.Convey("It should return +1 for even popcount", func() {
			even := primitive.NewValue()
			even[0] = 10
			even[0] |= 1 << 20

			gc.So(MobiusSign(even), gc.ShouldEqual, 1)
		})

		gc.Convey("It should return -1 for odd popcount", func() {
			odd := primitive.NewValue()
			odd[0] = 10
			odd[0] |= 1 << 20
			odd[0] |= 1 << 30

			gc.So(MobiusSign(odd), gc.ShouldEqual, -1)
		})

		gc.Convey("It should return +1 for the zero Value (empty product)", func() {
			gc.So(MobiusSign(primitive.NewValue()), gc.ShouldEqual, 1)
		})

		gc.Convey("It should return -1 for a single-bit Value", func() {
			single := primitive.NewValue()
			single[0] = 1 << 42

			gc.So(MobiusSign(single), gc.ShouldEqual, -1)
		})
	})
}

func TestDivides(t *testing.T) {
	gc.Convey("Given Values on the square-free lattice", t, func() {
		gc.Convey("It should confirm subset bits divide superset bits", func() {
			sub := primitive.NewValue()
			sub[0] = 1 << 10
			sub[0] |= 1 << 20

			sup := primitive.NewValue()
			sup[0] = 1 << 10
			sup[0] |= 1 << 20
			sup[0] |= 1 << 30

			gc.So(Divides(sub, sup), gc.ShouldBeTrue)
		})

		gc.Convey("It should reject when divisor has bits absent from numerator", func() {
			notSub := primitive.NewValue()
			notSub[0] = 1 << 10
			notSub[0] |= 1 << 20

			sup := primitive.NewValue()
			sup[0] = 1 << 10
			sup[0] |= 1 << 20

			gc.So(Divides(notSub, sup), gc.ShouldBeFalse)
		})

		gc.Convey("It should confirm the zero Value divides everything", func() {
			gc.So(Divides(primitive.NewValue(), primitive.NewValue()), gc.ShouldBeTrue)

			any := primitive.NewValue()
			any[0] = 1 << 5
			any[0] |= 1 << 20

			gc.So(Divides(primitive.NewValue(), any), gc.ShouldBeTrue)
		})
	})
}

func TestMobiusInvert(t *testing.T) {
	gc.Convey("Given an arithmetic function f and its aggregate g on a 3-bit lattice", t, func() {
		gc.Convey("It should exactly recover f from g", func() {
			bits := []int{10, 20, 30}

			fValues := map[uint64]int{
				0: 0, // {}
				1: 3, // {10}
				2: 5, // {20}
				4: 7, // {30}
				3: 0, // {10,20}
				5: 0, // {10,30}
				6: 0, // {20,30}
				7: 0, // {10,20,30}
			}

			gValues := make(map[uint64]int)
			for mask := uint64(0); mask < 8; mask++ {
				sum := 0

				for sub := uint64(0); sub < 8; sub++ {
					if sub&mask == sub {
						sum += fValues[sub]
					}
				}

				gValues[mask] = sum
			}

			aggregate := func(query *primitive.Value) int {
				mask := uint64(0)

				for idx, pos := range bits {
					if query[0]&(1<<pos) != 0 {
						mask |= 1 << idx
					}
				}

				return gValues[mask]
			}

			for mask := uint64(0); mask < 8; mask++ {
				target := SubsetValue(bits, mask)
				recovered := MobiusInvert(target, aggregate)

				gc.So(recovered, gc.ShouldEqual, fValues[mask])
			}
		})
	})
}

func TestContributorCounter(t *testing.T) {
	gc.Convey("Given contributors A={10,20,30} B={20,30,40} C={30,40,50}", t, func() {
		valA := primitive.NewValue()
		valA[0] = 1 << 10
		valA[0] |= 1 << 20
		valA[0] |= 1 << 30

		valB := primitive.NewValue()
		valB[0] = 1 << 20
		valB[0] |= 1 << 30
		valB[0] |= 1 << 40
		valB[0] |= 1 << 50

		valC := primitive.NewValue()
		valC[0] = 1 << 30
		valC[0] |= 1 << 40
		valC[0] |= 1 << 50
		valC[0] |= 1 << 60

		counter := ContributorCounter([]*primitive.Value{valA, valB, valC})

		gc.Convey("It should count zero contributors for a single-bit divisor", func() {
			single := primitive.NewValue()
			single[0] = 1 << 10

			gc.So(counter(single), gc.ShouldEqual, 0)
		})

		gc.Convey("It should count exactly one for A's exact bit set", func() {
			gc.So(counter(valA), gc.ShouldEqual, 1)
		})

		gc.Convey("It should count all three for the full composite", func() {
			full := primitive.NewValue()
			full[0] = 1 << 10
			full[0] |= 1 << 20
			full[0] |= 1 << 30
			full[0] |= 1 << 40
			full[0] |= 1 << 50

			gc.So(counter(full), gc.ShouldEqual, 3)
		})
	})
}

func BenchmarkMobiusInvert4Bits(b *testing.B) {
	target := primitive.NewValue()
	target[0] = 1 << 10
	target[0] |= 1 << 20
	target[0] |= 1 << 30
	target[0] |= 1 << 40

	aggregate := func(query *primitive.Value) int {
		return bits.OnesCount64(query[0])
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		MobiusInvert(target, aggregate)
	}
}

func BenchmarkMobiusInvert6Bits(b *testing.B) {
	target := primitive.NewValue()
	target[0] = 1 << 10
	target[0] |= 1 << 20
	target[0] |= 1 << 30
	target[0] |= 1 << 40
	target[0] |= 1 << 50
	target[0] |= 1 << 60

	aggregate := func(query *primitive.Value) int {
		return bits.OnesCount64(query[0])
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		MobiusInvert(target, aggregate)
	}
}
