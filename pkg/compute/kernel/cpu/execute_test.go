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

		Convey("Execute with an exact-binary OR frame should write the direct result to dst", func() {
			var frame [128]uint64
			frame[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR | 0x1
			frame[kernel.ProgramModeWord] = uint64(0) | (uint64(1) << 8)
			frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(kernel.AssetStartWord, 1)
			frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(kernel.AssetStartWord+1, 1)
			frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.PrevStartWord, 1)
			frame[kernel.AssetStartWord] = 0x00ff00ff00ff00ff
			frame[kernel.AssetStartWord+1] = 0x0f0f0f0f0f0f0f0f

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
			So(frame[kernel.PrevStartWord], ShouldEqual, uint64(0x0fff0fff0fff0fff))
		})

		Convey("Execute with a truth-table xor frame should succeed", func() {
			var frame [128]uint64
			const xorNibble = kernel.OpcodeXOR
			frame[kernel.ProgramOpcodeWord] = xorNibble
			// Sixteen-nibble rotation opcode table (same as programmer lowering).
			var packed uint64
			xorNibbleValue := xorNibble

			for rotation := 0; rotation < 16; rotation++ {
				packed |= xorNibbleValue << (rotation * 4)
			}

			frame[kernel.ProgramRotTabWord] = packed
			// Absolute region lanes the substrate reads / writes: operate
			// on tokens[0..16) for both operands and fold the 64-byte LSH
			// signature into affinity[0..5).
			frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
			frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
		})

		Convey("Execute with two xor frames should succeed", func() {
			var frameA, frameB [128]uint64
			frameA[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
			frameB[kernel.ProgramOpcodeWord] = 0x1

			// Both frames skip the universal-bitwise lane because their
			// rotation opcode table is zero — the test only exercises the
			// dispatch loop on two frames, not the ALU sweep itself.
			err := backend.ExecutePointers([]unsafe.Pointer{
				unsafe.Pointer(&frameA[0]),
				unsafe.Pointer(&frameB[0]),
			})

			So(err, ShouldBeNil)
		})

		Convey("Execute batch nearest-affinity path with one candidate", func() {
			var frame [128]uint64
			// Opcode XOR nibble and positive batch count selects batchAffinityDistances.
			frame[kernel.ProgramOpcodeWord] = kernel.OpcodeXOR
			frame[kernel.NearestAffinityBatchWord] = 1
			// Query (words 0–4) matches single candidate slab at word 56.
			for wordIdx := 0; wordIdx < 5; wordIdx++ {
				frame[wordIdx] = 0
				frame[kernel.NearestAffinityCandidatesStartWord+wordIdx] = 0
			}

			ptr := unsafe.Pointer(&frame[0])

			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
			So(frame[kernel.SignalsStartWord+kernel.SignalBestIdxOffset], ShouldEqual, uint64(0))
			So(frame[kernel.SignalsStartWord+kernel.SignalBestDistOffset], ShouldEqual, uint64(0))
		})
	})
}

func TestBackend_Execute_opcode0x40Frame(t *testing.T) {
	Convey("Given program low byte 0x40 (asset wire opcode; executes from asset region)", t, func() {
		backend := NewBackend(context.Background())

		var frame [128]uint64
		frame[kernel.ProgramOpcodeWord] = kernel.OpcodeRegionProgram

		// Write a dummy instruction into the asset region
		frame[kernel.AssetStartWord] = kernel.OpcodeXOR
		frame[kernel.AssetStartWord+1] = 0 // rotation table 0 means skip

		ptr := unsafe.Pointer(&frame[0])

		Convey("Execute should complete without error", func() {
			So(backend.ExecutePointers([]unsafe.Pointer{ptr}), ShouldBeNil)
		})
	})
}

func BenchmarkBackend_Execute_xorFrame(b *testing.B) {
	backend := NewBackend(context.Background())

	var frame [128]uint64
	const xorNibble = kernel.OpcodeXOR
	frame[kernel.ProgramOpcodeWord] = xorNibble

	var packed uint64
	xorNibbleValue := xorNibble

	for rotation := 0; rotation < 16; rotation++ {
		packed |= xorNibbleValue << (rotation * 4)
	}

	frame[kernel.ProgramRotTabWord] = packed
	frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
	frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
	frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)

	ptr := unsafe.Pointer(&frame[0])

	b.ResetTimer()

	for range b.N {
		_ = backend.ExecutePointers([]unsafe.Pointer{ptr})
	}
}
