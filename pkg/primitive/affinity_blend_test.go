package primitive

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestAffinityVectorIsZero(t *testing.T) {
	convey.Convey("AffinityVectorIsZero ignores high bits in the last word", t, func() {
		var z [AffinityWords]uint64

		convey.So(AffinityVectorIsZero(z), convey.ShouldBeTrue)

		z[0] = 1

		convey.So(AffinityVectorIsZero(z), convey.ShouldBeFalse)

		z[0] = 0
		z[AffinityWords-1] = 2

		convey.So(AffinityVectorIsZero(z), convey.ShouldBeTrue)

		z[AffinityWords-1] = 1

		convey.So(AffinityVectorIsZero(z), convey.ShouldBeFalse)
	})
}

func TestAffinityBlendPrunesInsteadOfHardRevert(t *testing.T) {
	setupAffinityTest(t)

	convey.Convey("Blend past Shannon headroom prunes instead of freezing", t, func() {
		a := NewAffinity()
		var vec [AffinityWords]uint64

		// Saturate toward the Shannon cap so the EMA blend reliably crosses it
		// and the prune path runs (Blend only prunes when popcount ≥ limit).
		for bit := 0; bit < 250; bit++ {
			vec[bit/64] |= 1 << uint(bit%64)
		}

		vec[AffinityWords-1] &= AffinityLastWordMask
		a.SetVector(vec)

		convey.So(a.Popcount(), convey.ShouldEqual, 250)

		inc := NewAffinity()
		inc.vector[0] = ^uint64(0)
		inc.vector[1] = ^uint64(0)
		inc.vector[2] = ^uint64(0)
		inc.vector[3] = ^uint64(0)
		inc.vector[AffinityWords-1] = 1

		shannonLimit := 240
		headroom := shannonLimit * 9 / 10

		if headroom < 1 {
			headroom = 1
		}

		next := a.Blend(inc, 40, shannonLimit)

		convey.So(next, convey.ShouldBeGreaterThan, uint64(40))
		convey.So(a.Popcount(), convey.ShouldBeLessThanOrEqualTo, headroom)
	})
}

func BenchmarkAffinityBlend(b *testing.B) {
	a := NewAffinity()
	inc := NewAffinity()

	var count uint64

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		count = a.Blend(inc, count, 240)
	}
}

func TestAffinityForNodeID(t *testing.T) {
	setupAffinityTest(t)

	convey.Convey("AffinityForNodeID is stable and masks the final word", t, func() {
		id := uint64(0xdeadbeefcafeb800)
		left := AffinityForNodeID(id)
		right := AffinityForNodeID(id)

		convey.So(left.Vector(), convey.ShouldResemble, right.Vector())
		convey.So(left.Vector()[AffinityWords-1]&^AffinityLastWordMask, convey.ShouldEqual, 0)
		convey.So(left.Vector()[AffinityWords-1], convey.ShouldEqual, id&AffinityLastWordMask)
	})
}
