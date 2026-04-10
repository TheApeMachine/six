package kernel

import (
	"math"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
)

func TestExecuteGeometricFrame(t *testing.T) {
	t.Parallel()

	Convey("Given a frame programmed with a geometric opcode", t, func() {
		Convey("It composes Context and Gradient into Signals", func() {
			var frame [128]uint64

			left := geometry.Rotor(math.Pi/2, 0, 0, 1)
			right := geometry.Translator(1, 2, 3)
			expected := left.GeometricProduct(right)

			frame[ProgramStartWord] = OpcodeGeometricCompose
			writeTestMultivector(&frame, ContextStartWord, left)
			writeTestMultivector(&frame, GradientStartWord, right)

			ok := ExecuteGeometricFrame(
				unsafe.Pointer(&frame),
				uint64(FrameProgramRawOpcode(unsafe.Pointer(&frame))),
			)

			So(ok, ShouldBeTrue)
			So(readTestMultivector(&frame, SignalsStartWord), ShouldResemble, expected)
		})

		Convey("It applies a motor sandwich from Context to Gradient", func() {
			var frame [128]uint64

			motor := geometry.Rotor(math.Pi/2, 0, 0, 1)
			target := geometry.Multivector{0, 0, 0, 0, 1, 0, 0, 0}
			expected := motor.Sandwich(target)

			frame[ProgramStartWord] = OpcodeGeometricSandwich
			writeTestMultivector(&frame, ContextStartWord, motor)
			writeTestMultivector(&frame, GradientStartWord, target)

			ok := ExecuteGeometricFrame(
				unsafe.Pointer(&frame),
				uint64(FrameProgramRawOpcode(unsafe.Pointer(&frame))),
			)

			So(ok, ShouldBeTrue)
			So(readTestMultivector(&frame, SignalsStartWord), ShouldResemble, expected)
		})

		Convey("It reverses the Context motor into Signals", func() {
			var frame [128]uint64

			motor := geometry.Multivector{1, 2, 3, 4, 5, 6, 7, 8}
			expected := motor.Reverse()

			frame[ProgramStartWord] = OpcodeGeometricReverse
			writeTestMultivector(&frame, ContextStartWord, motor)

			ok := ExecuteGeometricFrame(
				unsafe.Pointer(&frame),
				uint64(FrameProgramRawOpcode(unsafe.Pointer(&frame))),
			)

			So(ok, ShouldBeTrue)
			So(readTestMultivector(&frame, SignalsStartWord), ShouldResemble, expected)
		})
	})
}

func writeTestMultivector(frame *[128]uint64, start int, mv geometry.Multivector) {
	*(*geometry.Multivector)(unsafe.Pointer(&frame[start])) = mv
}

func readTestMultivector(frame *[128]uint64, start int) geometry.Multivector {
	return *(*geometry.Multivector)(unsafe.Pointer(&frame[start]))
}
