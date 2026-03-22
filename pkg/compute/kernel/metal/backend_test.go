package metal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/internaltest"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

func TestMetalBackend_Available(t *testing.T) {
	Convey("Given the Metal backend", t, func() {
		backend := NewBackend()
		count, err := backend.Available()

		if err != nil {
			So(count, ShouldEqual, 0)
			So(err, ShouldEqual, MetalErrorUnavailable)
			return
		}

		So(count, ShouldBeGreaterThanOrEqualTo, 1)
	})
}

func TestMetalBackend_BatchCorrectness(t *testing.T) {
	Convey("Given the Metal backend", t, func() {
		backend := NewBackend()
		count, err := backend.Available()

		if err != nil || count == 0 {
			t.Skip("Metal backend unavailable")
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
			So(dst, ShouldResemble, internaltest.ExpectedBinary(left, right, operation.AND))

			err = backend.BitwiseOr(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(dst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(dst, ShouldResemble, internaltest.ExpectedBinary(left, right, operation.OR))
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

func BenchmarkMetalBackend_BitwiseAnd(b *testing.B) {
	backend := NewBackend()
	if count, err := backend.Available(); err != nil || count == 0 {
		b.Skip("Metal backend unavailable")
	}

	for _, size := range []int{1, 16, 256, 1024} {
		left, right := internaltest.SampleBatch(size)
		dst := make([]primitive.Value, size)

		b.Run(benchmarkSizeLabel(size), func(b *testing.B) {
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
benchmarkSizeLabel converts the batch sizes used in this file.
*/
func benchmarkSizeLabel(size int) string {
	if size == 1 {
		return "batch_1"
	}
	if size == 16 {
		return "batch_16"
	}
	if size == 256 {
		return "batch_256"
	}

	return "batch_1024"
}
