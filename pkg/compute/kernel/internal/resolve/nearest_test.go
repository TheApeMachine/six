package resolve

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

func TestPackedNearest(t *testing.T) {
	Convey("Given PackedNearest", t, func() {
		Convey("It should return 0 for empty nodes", func() {
			packed := PackedNearest(nil, geometry.GFRotation{})
			So(packed, ShouldEqual, 0)
		})

		Convey("It should calculate nearest accurately", func() {
			nodes := []geometry.GFRotation{
				{CoordU: 10, CoordV: 10},
				{CoordU: 20, CoordV: 20},
				{CoordU: 30, CoordV: 30},
			}
			target := geometry.GFRotation{CoordU: 21, CoordV: 20}
			packed := PackedNearest(nodes, target)

			// Extract lowest 32 bits for idx
			idx := uint32(packed & 0xFFFFFFFF)
			So(idx, ShouldEqual, 1)
		})

		Convey("It should clamp max distance to avoid overflow", func() {
			nodes := []geometry.GFRotation{
				{CoordU: 0, CoordV: 0},
			}
			target := geometry.GFRotation{CoordU: 500, CoordV: 500}
			packed := PackedNearest(nodes, target)

			inverted := uint32(packed >> 32)
			So(inverted, ShouldEqual, 0)
			So(uint32(packed&0xFFFFFFFF), ShouldEqual, 0)
		})
	})
}

var benchResult uint64

func BenchmarkPackedNearest(b *testing.B) {
	b.Run("small", func(b *testing.B) {
		nodes := []geometry.GFRotation{
			{CoordU: 10, CoordV: 10},
			{CoordU: 20, CoordV: 20},
			{CoordU: 30, CoordV: 30},
		}
		target := geometry.GFRotation{CoordU: 21, CoordV: 20}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchResult = PackedNearest(nodes, target)
		}
	})
	b.Run("large", func(b *testing.B) {
		nodes := make([]geometry.GFRotation, 10000)
		for i := range nodes {
			nodes[i] = geometry.GFRotation{
				CoordU: uint16((i * 17) % 257),
				CoordV: uint16((i * 31) % 257),
			}
		}
		target := geometry.GFRotation{CoordU: 97, CoordV: 143}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchResult = PackedNearest(nodes, target)
		}
	})
	b.Run("distant", func(b *testing.B) {
		nodes := make([]geometry.GFRotation, 1000)
		for i := range nodes {
			nodes[i] = geometry.GFRotation{CoordU: 0, CoordV: 0}
		}
		target := geometry.GFRotation{CoordU: 500, CoordV: 500}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchResult = PackedNearest(nodes, target)
		}
	})
}
