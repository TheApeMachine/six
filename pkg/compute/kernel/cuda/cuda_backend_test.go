//go:build cuda && cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

func TestCUDABackendResolve(t *testing.T) {
	Convey("Given the CUDA backend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("It should match the kernel packing contract", func() {
			nodes := []geometry.GFRotation{
				{CoordU: 1, CoordV: 0},
				{CoordU: 21, CoordV: 34},
				{CoordU: 55, CoordV: 89},
			}
			target := geometry.GFRotation{CoordU: 20, CoordV: 34}

			packed, err := backend.Resolve(
				unsafe.Pointer(&nodes[0]),
				len(nodes),
				unsafe.Pointer(&target),
			)

			bestIdx, distSq := kernel.DecodePacked(packed)

			So(err, ShouldBeNil)
			So(backend.Available(), ShouldBeTrue)
			So(bestIdx, ShouldEqual, 1)
			So(distSq, ShouldEqual, 1)
		})
	})
}

func BenchmarkCUDABackendResolve(b *testing.B) {
	backend := &cuda.CUDABackend{}
	if !backend.Available() {
		b.Skip("CUDA backend unavailable")
	}

	nodeCount := 10000
	nodes := make([]geometry.GFRotation, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = geometry.GFRotation{
			CoordU: uint16((i * 17) % 257),
			CoordV: uint16((i * 31) % 257),
		}
	}
	target := nodes[42]

	b.ResetTimer()
	for b.Loop() {
		_, _ = backend.Resolve(
			unsafe.Pointer(&nodes[0]),
			len(nodes),
			unsafe.Pointer(&target),
		)
	}
}
