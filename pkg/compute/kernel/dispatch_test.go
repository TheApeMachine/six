package kernel

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

func generateGFRotations(count int) []geometry.GFRotation {
	nodes := make([]geometry.GFRotation, count)
	for i := 0; i < count; i++ {
		nodes[i] = geometry.GFRotation{
			CoordU: uint16((i * 17) % 257),
			CoordV: uint16((i * 31) % 257),
		}
	}
	return nodes
}

func TestNewBuilder(t *testing.T) {
	Convey("Given the default kernel builder", t, func() {
		t.Setenv("SIX_BACKEND", "cpu")

		builder := NewBuilder()

		Convey("It should resolve through an available backend", func() {
			nodes := generateGFRotations(100)
			target := nodes[42]

			packed, err := builder.Resolve(
				unsafe.Pointer(&nodes[0]),
				len(nodes),
				unsafe.Pointer(&target),
			)

			bestIdx, distSq := DecodePacked(packed)

			So(err, ShouldBeNil)
			So(builder.Available(), ShouldBeTrue)
			So(bestIdx, ShouldEqual, 42)
			So(distSq, ShouldEqual, 0)
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
