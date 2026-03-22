//go:build !cuda || !cgo

package cuda_test

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCUDABackend_Available(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.Backend{}

		Convey("It should probe NVML for GPU count", func() {
			n, err := backend.Available()

			if err != nil {
				So(n, ShouldEqual, 0)
				So(err, ShouldHaveSameTypeAs, cuda.CUDAErrorUnavailable)
				So(err.Error(), ShouldEqual, string(cuda.CUDAErrorUnavailable))

				return
			}

			So(n, ShouldBeGreaterThanOrEqualTo, 0)
		})
	})
}

func TestCUDABackend_OperationsUnavailable(t *testing.T) {
	Convey("Given a stubbed CUDABackend", t, func() {
		backend := &cuda.Backend{}
		left := []primitive.Value{*primitive.BaseValue(30)}
		right := []primitive.Value{*primitive.BaseValue(70)}
		dst := make([]primitive.Value, 1)

		Convey("Kernel dispatch methods should return CUDAErrorUnavailable", func() {
			So(
				backend.BitwiseAnd(
					unsafe.Pointer(&left[0]),
					unsafe.Pointer(&right[0]),
					unsafe.Pointer(&dst[0]),
					1,
				),
				ShouldEqual,
				cuda.CUDAErrorUnavailable,
			)
			So(
				backend.MotorApply(
					unsafe.Pointer(&left[0]),
					unsafe.Pointer(&right[0]),
					unsafe.Pointer(&dst[0]),
					1,
				),
				ShouldEqual,
				cuda.CUDAErrorUnavailable,
			)
			So(
				backend.RollLeft(
					unsafe.Pointer(&left[0]),
					unsafe.Pointer(&dst[0]),
					7,
					1,
				),
				ShouldEqual,
				cuda.CUDAErrorUnavailable,
			)
		})
	})
}

func BenchmarkCUDABackend_Available(b *testing.B) {
	backend := &cuda.Backend{}

	for b.Loop() {
		backend.Available()
	}
}
