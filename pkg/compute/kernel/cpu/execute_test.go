package cpu

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/program"
)

func TestNewBackend(t *testing.T) {
	Convey("Given a background context", t, func() {
		backend := NewBackend(context.Background())

		Convey("NewBackend should return a non-nil Backend", func() {
			So(backend, ShouldNotBeNil)
			So(backend.ctx, ShouldNotBeNil)
		})

		Convey("Name should be cpu", func() {
			So(backend.Name(), ShouldEqual, "cpu")
		})

		Convey("Available should be positive on real hardware", func() {
			So(Available(), ShouldBeGreaterThan, 0)
		})
	})
}

// xorAffinityInstruction is the canonical "fold all 16 token words against
// themselves with XOR and write the 64-byte signature into the affinity
// region" sweep, matching the new packed instruction format.
func xorAffinityInstruction() uint64 {
	return program.EncodeInstruction(
		kernel.TokensStartWord, 16,
		kernel.TokensStartWord, 16,
		kernel.AffinityStartWord, 5,
		kernel.OpcodeXOR,
		program.ModeAccumulate,
	)
}

func TestBackend_Execute(t *testing.T) {
	Convey("Given a CPU Backend", t, func() {
		backend := NewBackend(context.Background())

		Convey("Execute with nil slice should return nil", func() {
			So(backend.Execute(nil), ShouldBeNil)
		})

		Convey("Execute with empty slice should return nil", func() {
			So(backend.Execute([]uint32{}), ShouldBeNil)
		})

		Convey("ExecutePointers with a nil frame pointer should return KernelErrNilPointer", func() {
			err := backend.ExecutePointers([]unsafe.Pointer{nil})

			So(err, ShouldNotBeNil)

			var ke *kernel.KernelError

			So(errors.As(err, &ke), ShouldBeTrue)
			So(ke.Type, ShouldEqual, kernel.KernelErrNilPointer)
		})

		Convey("Execute with a single XOR sweep instruction should succeed", func() {
			var frame [128]uint64
			frame[kernel.ProgramStartWord] = program.EncodeInstruction(
				kernel.TokensStartWord, 16,
				kernel.TokensStartWord, 16,
				kernel.AffinityStartWord, 5,
				kernel.OpcodeXOR,
				program.ModeAccumulate,
			)

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
		})

		Convey("Execute with two frames should succeed", func() {
			var frameA, frameB [128]uint64
			frameA[kernel.ProgramStartWord] = program.EncodeInstruction(
				kernel.TokensStartWord, 16,
				kernel.TokensStartWord, 16,
				kernel.AffinityStartWord, 5,
				kernel.OpcodeXOR,
				program.ModeAccumulate,
			)
			frameB[kernel.ProgramStartWord] = program.EncodeInstruction(
				kernel.TokensStartWord, 16,
				kernel.TokensStartWord, 16,
				kernel.AffinityStartWord, 5,
				kernel.OpcodeAND,
				program.ModeAccumulate,
			)

			err := backend.ExecutePointers([]unsafe.Pointer{
				unsafe.Pointer(&frameA[0]),
				unsafe.Pointer(&frameB[0]),
			})

			So(err, ShouldBeNil)
		})

		Convey("Empty program (zero word) should be a no-op", func() {
			var frame [128]uint64

			err := backend.ExecutePointers([]unsafe.Pointer{unsafe.Pointer(&frame[0])})

			So(err, ShouldBeNil)
		})
	})
}

func BenchmarkBackend_Execute_xorFrame(b *testing.B) {
	backend := NewBackend(context.Background())

	var frame [128]uint64
	frame[kernel.ProgramStartWord] = xorAffinityInstruction()

	ptr := unsafe.Pointer(&frame[0])

	b.ResetTimer()

	for range b.N {
		_ = backend.ExecutePointers([]unsafe.Pointer{ptr})
	}
}
