//go:build cuda && cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/geometry"
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
