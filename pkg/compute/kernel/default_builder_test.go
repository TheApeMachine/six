package kernel

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

func TestNewBuilder_DefaultFallbackDoesNotPanicOnCPUHost(t *testing.T) {
	Convey("Given the default kernel builder on a CPU/default host", t, func() {
		builder := NewBuilder()
		nodes := []geometry.GFRotation{
			{CoordU: 1, CoordV: 2},
			{CoordU: 8, CoordV: 13},
			{CoordU: 21, CoordV: 34},
		}
		target := geometry.GFRotation{CoordU: 8, CoordV: 13}

		Convey("When resolving nodes", func() {
			packed, err := builder.Resolve(
				unsafe.Pointer(&nodes[0]),
				len(nodes),
				unsafe.Pointer(&target),
			)
			
			Convey("It returns the correct best index and distance", func() {
				bestIdx, distSq := DecodePacked(packed)

				So(err, ShouldBeNil)
				So(builder.Available(), ShouldBeTrue)
				So(bestIdx, ShouldEqual, 1)
				So(distSq, ShouldEqual, 0)
			})
		})
	})
}

var sinkIdx int
var sinkDistSq float64

func BenchmarkNewBuilder_Resolve(b *testing.B) {
	builder := NewBuilder()
	nodes := []geometry.GFRotation{
		{CoordU: 1, CoordV: 2},
		{CoordU: 8, CoordV: 13},
		{CoordU: 21, CoordV: 34},
	}
	target := geometry.GFRotation{CoordU: 8, CoordV: 13}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packed, _ := builder.Resolve(
			unsafe.Pointer(&nodes[0]),
			len(nodes),
			unsafe.Pointer(&target),
		)
		sinkIdx, sinkDistSq = DecodePacked(packed)
	}
}
