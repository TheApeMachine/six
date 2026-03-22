package cpu

import (
	"runtime"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/internaltest"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

func TestCPUBackend_Available(t *testing.T) {
	Convey("Given a CPU backend", t, func() {
		backend := &Backend{}

		Convey("It should return the number of logical CPU cores", func() {
			n, err := backend.Available()

			So(err, ShouldBeNil)
			So(n, ShouldEqual, runtime.NumCPU())
		})
	})
}

func TestCPUBackend_BatchCorrectness(t *testing.T) {
	Convey("Given the CPU backend and deterministic Value batches", t, func() {
		backend := NewBackend()
		left, right := internaltest.SampleBatch(32)

		Convey("Bitwise binary operations should match primitive truth exactly", func() {
			cases := []struct {
				name     string
				run      func(dst []primitive.Value) error
				expected []primitive.Value
			}{
				{
					name: "OR",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseOr(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.OR),
				},
				{
					name: "AND",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseAnd(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.AND),
				},
				{
					name: "XOR",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseXor(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.XOR),
				},
				{
					name: "AndNot",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseAndNot(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.AndNot),
				},
				{
					name: "NAND",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseNand(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.NAND),
				},
				{
					name: "NOR",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseNor(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.NOR),
				},
				{
					name: "XNOR",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseXnor(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.XNOR),
				},
				{
					name: "ConverseNonimplication",
					run: func(dst []primitive.Value) error {
						return backend.BitwiseConverseNonimplication(
							internaltest.ValuesPointer(left),
							internaltest.ValuesPointer(right),
							internaltest.ValuesPointer(dst),
							uint32(len(left)),
						)
					},
					expected: internaltest.ExpectedBinary(left, right, operation.ConverseNonimplication),
				},
			}

			for _, testCase := range cases {
				dst := make([]primitive.Value, len(left))
				err := testCase.run(dst)

				So(err, ShouldBeNil)
				So(dst, ShouldResemble, testCase.expected)
			}
		})

		Convey("Unary and motor operations should match primitive truth exactly", func() {
			notDst := make([]primitive.Value, len(left))
			err := backend.BitwiseNot(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(notDst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(notDst, ShouldResemble, internaltest.ExpectedUnary(left, operation.NOT))

			motorApplyDst := make([]primitive.Value, len(left))
			err = backend.MotorApply(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(motorApplyDst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(motorApplyDst, ShouldResemble, internaltest.ExpectedMotorApply(left, right))

			motorInvertDst := make([]primitive.Value, len(left))
			err = backend.MotorInvert(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(motorInvertDst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(motorInvertDst, ShouldResemble, internaltest.ExpectedMotorInvert(left, right))

			motorComposeDst := make([]primitive.Value, len(left))
			err = backend.MotorCompose(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(right),
				internaltest.ValuesPointer(motorComposeDst),
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(motorComposeDst, ShouldResemble, internaltest.ExpectedMotorCompose(left, right))

			rollDst := make([]primitive.Value, len(left))
			err = backend.RollLeft(
				internaltest.ValuesPointer(left),
				internaltest.ValuesPointer(rollDst),
				97,
				uint32(len(left)),
			)
			So(err, ShouldBeNil)
			So(rollDst, ShouldResemble, internaltest.ExpectedRollLeft(left, 97))
		})
	})
}

func BenchmarkCPUBackend_BitwiseAnd(b *testing.B) {
	backend := NewBackend()

	for _, size := range []int{1, 16, 256, 1024} {
		left, right := internaltest.SampleBatch(size)
		dst := make([]primitive.Value, size)

		b.Run("batch_"+itoa(size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = backend.BitwiseAnd(
					unsafe.Pointer(&left[0]),
					unsafe.Pointer(&right[0]),
					unsafe.Pointer(&dst[0]),
					uint32(size),
				)
			}
		})
	}
}

func BenchmarkCPUBackend_MotorApply(b *testing.B) {
	backend := NewBackend()

	for _, size := range []int{1, 16, 256, 1024} {
		left, right := internaltest.SampleBatch(size)
		dst := make([]primitive.Value, size)

		b.Run("batch_"+itoa(size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				_ = backend.MotorApply(
					unsafe.Pointer(&left[0]),
					unsafe.Pointer(&right[0]),
					unsafe.Pointer(&dst[0]),
					uint32(size),
				)
			}
		})
	}
}

/*
itoa converts the small benchmark batch sizes used in this file.
*/
func itoa(value int) string {
	if value == 1 {
		return "1"
	}
	if value == 16 {
		return "16"
	}
	if value == 256 {
		return "256"
	}

	return "1024"
}
