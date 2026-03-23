//go:build cuda && cgo

package cuda

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/internaltest"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCUDABackend_BatchCorrectness(t *testing.T) {
	Convey("Given the CUDA backend", t, func() {
		backend := NewBackend()
		count, err := backend.Available()

		if err != nil || count == 0 {
			t.Skip("CUDA backend unavailable")
		}

		left, right := internaltest.SampleBatch(32)

		Convey("Bitwise operations should match primitive truth exactly", func() {
			dst := make([]primitive.Value, len(left))

			err := backend.BitwiseAnd(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(dst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(dst, ShouldResemble, internaltest.ExpectedBinary(left, right, func(a, b uint64) uint64 { return a & b }))

			err = backend.BitwiseXor(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(dst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(dst, ShouldResemble, internaltest.ExpectedBinary(left, right, operation.XOR))
		})

		Convey("MotorApply and RollLeft should match primitive truth exactly", func() {
			motorDst := make([]primitive.Value, len(left))
			err := backend.MotorApply(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(motorDst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(motorDst, ShouldResemble, internaltest.ExpectedMotorApply(left, right))

			rollDst := make([]primitive.Value, len(left))
			err = backend.RollLeft(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(rollDst),
				31,
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(rollDst, ShouldResemble, internaltest.ExpectedRollLeft(left, 31))
		})
	})
}

func BenchmarkCUDABackend_BitwiseAnd(b *testing.B) {
	backend := NewBackend()
	if count, err := backend.Available(); err != nil || count == 0 {
		b.Skip("CUDA backend unavailable")
	}

	for _, size := range []int{1, 16, 256, 1024} {
		left, right := internaltest.SampleBatch(size)
		dst := make([]primitive.Value, size)

		b.Run("batch_"+benchmarkLabel(size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = backend.BitwiseAnd(
					internaltest.ValuesPointer(left),
					internaltest.ValuesPointer(right),
					internaltest.ValuesPointer(dst),
					uint32(size),
				)
			}
		})
	}
}

/*
benchmarkLabel converts the small batch sizes used in this file.
*/
func benchmarkLabel(size int) string {
	if size == 1 {
		return "1"
	}
	if size == 16 {
		return "16"
	}
	if size == 256 {
		return "256"
	}

	return "1024"
}
