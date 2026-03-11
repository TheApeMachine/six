package kernel

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/geometry"
)

func TestNewBuilder(t *testing.T) {
	Convey("Given the default kernel builder", t, func() {
		t.Setenv("SIX_BACKEND", "cpu")

		builder := NewBuilder()

		Convey("It should resolve through an available backend", func() {
			nodes := []geometry.GFRotation{
				{A: 1, B: 0},
				{A: 5, B: 8},
				{A: 9, B: 13},
			}
			target := geometry.GFRotation{A: 6, B: 8}

			packed, err := builder.Resolve(
				unsafe.Pointer(&nodes[0]),
				len(nodes),
				unsafe.Pointer(&target),
			)

			bestIdx, distSq := DecodePacked(packed)

			So(err, ShouldBeNil)
			So(builder.Available(), ShouldBeTrue)
			So(bestIdx, ShouldEqual, 1)
			So(distSq, ShouldEqual, 1)
		})
	})
}

// func BenchmarkBuilderResolve(b *testing.B) {
// 	nodes := make([]geometry.GFRotation, 4096)

// 	for idx := range nodes {
// 		nodes[idx] = geometry.GFRotation{
// 			A: uint16((idx % 256) + 1),
// 			B: uint16((idx * 17) % geometry.CubeFaces),
// 		}
// 	}

// 	target := geometry.GFRotation{A: 97, B: 143}
// 	builder := NewBuilder()
// 	b.ResetTimer()

// 	for b.Loop() {
// 		_, _ = builder.Resolve(
// 			unsafe.Pointer(&nodes[0]),
// 			len(nodes),
// 			unsafe.Pointer(&target),
// 		)
// 	}
// }
