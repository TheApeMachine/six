//go:build !cuda || !cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/geometry"
	"github.com/theapemachine/six/pkg/kernel/cuda"
)

func TestCUDABackend_Available(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("Available should return false", func() {
			So(backend.Available(), ShouldBeFalse)
		})
	})
}

func TestCUDABackend_Resolve(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("Resolve should return 0", func() {
			val, err := backend.Resolve(unsafe.Pointer(&geometry.GFRotation{}), 0, unsafe.Pointer(&geometry.GFRotation{}))
			So(val, ShouldEqual, 0)
			So(err, ShouldBeNil)
		})
	})
}
