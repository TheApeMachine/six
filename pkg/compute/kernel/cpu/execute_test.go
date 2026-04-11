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
			So(backend.Execute([]unsafe.Pointer{}), ShouldBeNil)
		})

		Convey("Execute with a nil frame pointer should return KernelErrNilPointer", func() {
			err := backend.Execute([]unsafe.Pointer{nil})

			So(err, ShouldNotBeNil)

			var ke *kernel.KernelError

			So(errors.As(err, &ke), ShouldBeTrue)
			So(ke.Type, ShouldEqual, kernel.KernelErrNilPointer)
		})

		Convey("Execute with a truth-table xor frame should succeed", func() {
			var frame [128]uint64
			const xorNibble = kernel.OpcodeXOR
			frame[kernel.ProgramStartWord] = xorNibble
			// Second program word: sixteen nibbles for universalBitwiseV2 decode (same as programmer lowering).
			var packed uint64
			xorNibbleValue := xorNibble

			for rotation := 0; rotation < 16; rotation++ {
				packed |= xorNibbleValue << (rotation * 4)
			}

			frame[kernel.ProgramStartWord+1] = packed

			ptr := unsafe.Pointer(&frame[0])

			So(backend.Execute([]unsafe.Pointer{ptr}), ShouldBeNil)
		})

		Convey("Execute with two xor frames should succeed", func() {
			var frameA, frameB [128]uint64
			frameA[kernel.ProgramStartWord] = kernel.OpcodeXOR
			frameB[kernel.ProgramStartWord] = 0x1

			err := backend.Execute([]unsafe.Pointer{
				unsafe.Pointer(&frameA[0]),
				unsafe.Pointer(&frameB[0]),
			})

			So(err, ShouldBeNil)
		})

		Convey("Execute batch nearest-affinity path with one candidate", func() {
			var frame [128]uint64
			// Opcode XOR nibble and positive batch count selects batchAffinityDistances.
			frame[kernel.ProgramStartWord] = kernel.OpcodeXOR
			frame[kernel.NearestAffinityBatchWord] = 1
			// Query (words 0–4) matches single candidate slab at word 56.
			for wordIdx := 0; wordIdx < 5; wordIdx++ {
				frame[wordIdx] = 0
				frame[kernel.NearestAffinityCandidatesStartWord+wordIdx] = 0
			}

			ptr := unsafe.Pointer(&frame[0])

			So(backend.Execute([]unsafe.Pointer{ptr}), ShouldBeNil)
			So(frame[kernel.SignalsStartWord+kernel.SignalBestIdxOffset], ShouldEqual, uint64(0))
			So(frame[kernel.SignalsStartWord+kernel.SignalBestDistOffset], ShouldEqual, uint64(0))
		})
	})
}

func TestBackend_Execute_opcode0x40Frame(t *testing.T) {
	Convey("Given program low byte 0x40 (reserved wire opcode; dispatch falls through)", t, func() {
		backend := NewBackend(context.Background())

		var frame [128]uint64
		frame[kernel.ProgramStartWord] = kernel.OpcodeRegionProgram

		ptr := unsafe.Pointer(&frame[0])

		Convey("Execute should complete without error", func() {
			So(backend.Execute([]unsafe.Pointer{ptr}), ShouldBeNil)
		})
	})
}

func BenchmarkBackend_Execute_xorFrame(b *testing.B) {
	backend := NewBackend(context.Background())

	var frame [128]uint64
	const xorNibble = kernel.OpcodeXOR
	frame[kernel.ProgramStartWord] = xorNibble

	var packed uint64
	xorNibbleValue := xorNibble

	for rotation := 0; rotation < 16; rotation++ {
		packed |= xorNibbleValue << (rotation * 4)
	}

	frame[kernel.ProgramStartWord+1] = packed

	ptr := unsafe.Pointer(&frame[0])

	b.ResetTimer()

	for range b.N {
		_ = backend.Execute([]unsafe.Pointer{ptr})
	}
}
