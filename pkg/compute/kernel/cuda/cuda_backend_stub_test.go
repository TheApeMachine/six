//go:build !cuda || !cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/numeric/geometry"
)

func TestCUDABackend_Available(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("It should return false for Available", func() {
			So(backend.Available(), ShouldBeFalse)
		})
	})
}

func TestCUDABackend_Resolve(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.CUDABackend{}

		Convey("It should return 0 and no error for Resolve", func() {
			val, err := backend.Resolve(unsafe.Pointer(&geometry.GFRotation{}), 0, unsafe.Pointer(&geometry.GFRotation{}))
			So(val, ShouldEqual, 0)
			So(err, ShouldBeNil)
		})
	})
}

func BenchmarkCUDABackend_Available(b *testing.B) {
	backend := &cuda.CUDABackend{}
	for b.Loop() {
		backend.Available()
	}
}

func BenchmarkCUDABackend_Resolve(b *testing.B) {
	backend := &cuda.CUDABackend{}
	rotation := &geometry.GFRotation{}
	for b.Loop() {
		backend.Resolve(unsafe.Pointer(rotation), 0, unsafe.Pointer(rotation))
	}
}
