package primitive

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestAffinityBlendPrunesInsteadOfHardRevert(t *testing.T) {
	setupAffinityTest(t)

	convey.Convey("Blend past Shannon headroom prunes instead of freezing", t, func() {
		a := NewAffinity()
		var vec [AffinityWords]uint64

		for bit := 0; bit < 236; bit++ {
			vec[bit/64] |= 1 << uint(bit%64)
		}

		vec[AffinityWords-1] &= AffinityLastWordMask
		a.SetVector(vec)

		convey.So(a.Popcount(), convey.ShouldEqual, 236)

		inc := NewAffinity()
		inc.vector[0] = ^uint64(0)
		inc.vector[1] = ^uint64(0)
		inc.vector[2] = ^uint64(0)
		inc.vector[3] = ^uint64(0)
		inc.vector[AffinityWords-1] = 1

		next := a.Blend(inc, 40, 240)

		convey.So(next, convey.ShouldBeGreaterThan, uint64(40))
		convey.So(a.Popcount(), convey.ShouldBeLessThan, 240)
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
