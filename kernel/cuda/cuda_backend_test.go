//go:build cuda && cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/kernel/cuda"
)

func TestCUDABackendResolve(t *testing.T) {
	Convey("Given the CUDA backend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("It should match the kernel packing contract", func() {
			nodes := []geometry.GFRotation{
				{A: 1, B: 0},
				{A: 21, B: 34},
				{A: 55, B: 89},
			}
			target := geometry.GFRotation{A: 20, B: 34}

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
