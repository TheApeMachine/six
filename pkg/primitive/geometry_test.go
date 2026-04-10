package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewGeometry(t *testing.T) {
	Convey("Given an explicit lattice shape", t, func() {
		geometry := newGeometry(32, 32, 4)

		Convey("It should preserve the dimensionality and shape", func() {
			So(geometry.Dimensions(), ShouldEqual, 3)
			So(geometry.Shape(), ShouldResemble, []uint32{32, 32, 4})
		})

		Convey("It should project ordinals into mixed-radix coordinates", func() {
			So(geometry.Coordinates(0), ShouldResemble, []uint32{0, 0, 0})
			So(geometry.Coordinates(1), ShouldResemble, []uint32{1, 0, 0})
			So(geometry.Coordinates(32), ShouldResemble, []uint32{0, 1, 0})
			So(geometry.Coordinates(32*32), ShouldResemble, []uint32{0, 0, 1})
		})
	})

	Convey("Given a degenerate shape", t, func() {
		geometry := newGeometry(0)

		Convey("It should normalize to a valid Morton lattice", func() {
			So(geometry.Dimensions(), ShouldEqual, 2)
			So(geometry.Shape(), ShouldResemble, []uint32{1, 1})
		})
	})
}

func TestNewBalancedGeometry(t *testing.T) {
	Convey("Given a segment-sized point budget", t, func() {
		geometry := newBalancedGeometry(64, 2)

		Convey("It should choose a balanced lattice", func() {
			So(geometry.Shape(), ShouldResemble, []uint32{8, 8})
		})

		Convey("It should yield unique position codes across the default segment", func() {
			seen := make(map[uint8]struct{}, 64)

			for ordinal := range 64 {
				code := geometry.PositionCode(uint32(ordinal))
				seen[code] = struct{}{}
			}

			So(len(seen), ShouldEqual, 64)
		})
	})
}

func BenchmarkNewGeometry(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = newGeometry(32, 32, 4)
	}
}

func BenchmarkNewBalancedGeometry(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = newBalancedGeometry(64, 2)
	}
}

func BenchmarkGeometryCoordinates(b *testing.B) {
	g := newBalancedGeometry(64, 2)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = g.Coordinates(uint32(iteration % 64))
	}
}

func BenchmarkGeometryPositionCode(b *testing.B) {
	g := newBalancedGeometry(64, 2)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = g.PositionCode(uint32(iteration % 64))
	}
}
