package kernel

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
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
		builder := NewBuilder(WithBackend(&cpu.CPUBackend{}))

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

func BenchmarkBuilderResolve(b *testing.B) {
	nodes := generateGFRotations(4096)
	target := nodes[42]
	builder := NewBuilder(WithBackend(&cpu.CPUBackend{}))
	b.ResetTimer()

	for b.Loop() {
		_, _ = builder.Resolve(
			unsafe.Pointer(&nodes[0]),
			len(nodes),
			unsafe.Pointer(&target),
		)
	}
}
