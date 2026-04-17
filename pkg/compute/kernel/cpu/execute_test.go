package cpu

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
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

		Convey("Execute with a truth-table xor frame should succeed", func() {
			var frame [128]uint64
			frame[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
			frame[kernel.ProgramModeWord] = 0
			frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
		})

		Convey("Execute with xor opcode broadcasts low nibble (no separate rotation word)", func() {
			var frame [128]uint64
			frame[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
			frame[kernel.ProgramModeWord] = 0
			frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
		})

		Convey("Execute with two frames and zero rotation tables should succeed", func() {
			var frameA, frameB [128]uint64
			frameA[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
			frameB[kernel.ProgramOpcodeWord] = 0x1

			err := backend.ExecutePointers([]unsafe.Pointer{
				unsafe.Pointer(&frameA[0]),
				unsafe.Pointer(&frameB[0]),
			})

			So(err, ShouldBeNil)
		})
	})
}

func BenchmarkBackend_Execute_xorFrame(b *testing.B) {
	backend := NewBackend(context.Background())

	var frame [128]uint64
	frame[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
	frame[kernel.ProgramModeWord] = 0
	frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
	frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
	frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)

	ptr := unsafe.Pointer(&frame[0])

	b.ResetTimer()

	for range b.N {
		_ = backend.ExecutePointers([]unsafe.Pointer{ptr})
	}
}
