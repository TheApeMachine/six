package cpu

import (
	"context"
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
)

func init() {
	// Tests run without viper/config.yml, so set the layout manually.
	core.Cfg.Value.Region.Program.Start = 76
	core.Cfg.Value.Region.Program.Bits = 3328 // 52 words * 64
	core.Cfg.Value.Region.State.Accumulator = 62
	core.Cfg.Value.Region.State.Sequence = 61
	core.Cfg.Value.Region.Registers.FW = 74
	core.Cfg.Value.Region.Registers.PC = 75
}

// encode32 builds a 32-bit LGP instruction: [3:0] op, [17:4] src, [31:18] dst.
func encode32(op uint8, src, dst int) uint32 {
	return uint32(op&0xF) | uint32(src&0x3FFF)<<4 | uint32(dst&0x3FFF)<<18
}

// installSlot writes a 32-bit instruction into the program region of a frame.
func installSlot(frame *[128]uint64, slot int, instr uint32) {
	wordIdx := core.Cfg.Value.Region.Program.Start + slot/2
	shift := uint((slot % 2) * 32)
	mask := uint64(0xFFFFFFFF) << shift
	frame[wordIdx] = (frame[wordIdx] &^ mask) | (uint64(instr) << shift)
}

func TestUniversalBitwiseTruthTable(t *testing.T) {
	convey.Convey("Given the branchless 32-bit LGP pipeline", t, func() {
		backend := NewBackend(context.Background())

		convey.Convey("AND (op=0x1): src=0, dst=1", func() {
			var f [128]uint64
			f[0] = 0xAAAAAAAAAAAAAAAA // src word (A)
			f[1] = 0xCCCCCCCCCCCCCCCC // dst word (B)
			installSlot(&f, 0, encode32(0x1, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0x8888888888888888))
		})

		convey.Convey("XOR (op=0x6): src=0, dst=1", func() {
			var f [128]uint64
			f[0] = 0xAAAAAAAAAAAAAAAA
			f[1] = 0xCCCCCCCCCCCCCCCC
			installSlot(&f, 0, encode32(0x6, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0x6666666666666666))
		})

		convey.Convey("OR (op=0x7): src=0, dst=1", func() {
			var f [128]uint64
			f[0] = 0xAAAAAAAAAAAAAAAA
			f[1] = 0xCCCCCCCCCCCCCCCC
			installSlot(&f, 0, encode32(0x7, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0xEEEEEEEEEEEEEEEE))
		})

		convey.Convey("FALSE (op=0x0): dst zeroed", func() {
			var f [128]uint64
			f[0] = 0xFFFFFFFFFFFFFFFF
			f[1] = 0xFFFFFFFFFFFFFFFF
			installSlot(&f, 0, encode32(0x0, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0))
		})

		convey.Convey("TRUE (op=0xF): dst all ones", func() {
			var f [128]uint64
			f[0] = 0
			f[1] = 0
			installSlot(&f, 0, encode32(0xF, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, ^uint64(0))
		})

		convey.Convey("COPY A (op=0x3): dst = src", func() {
			var f [128]uint64
			f[0] = 0xDEADBEEFDEADBEEF
			f[1] = 0
			installSlot(&f, 0, encode32(0x3, 0, 1))

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0xDEADBEEFDEADBEEF))
		})

		convey.Convey("NOP slots are skipped, data unchanged", func() {
			var f [128]uint64
			f[1] = 0x1234
			// No instructions installed — all slots are 0 (NOP).
			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(f[1], convey.ShouldEqual, uint64(0x1234))
		})
	})
}

func TestUniversalBitwiseMultipleFrames(t *testing.T) {
	convey.Convey("Multiple frames execute the same program via SIMD gather-scatter", t, func() {
		backend := NewBackend(context.Background())

		n := 64
		frames := make([][128]uint64, n)
		ptrs := make([]unsafe.Pointer, n)

		for i := range frames {
			frames[i][0] = uint64(i)                        // src: different per frame
			frames[i][1] = 0xFFFFFFFFFFFFFFFF               // dst: same
			installSlot(&frames[i], 0, encode32(0x1, 0, 1)) // AND
			ptrs[i] = unsafe.Pointer(&frames[i])
		}

		err := backend.UniversalBitwise(ptrs)
		convey.So(err, convey.ShouldBeNil)

		for i := range frames {
			expected := uint64(i) & 0xFFFFFFFFFFFFFFFF
			convey.So(frames[i][1], convey.ShouldEqual, expected)
		}
	})
}

func TestUniversalBitwiseHeterogeneousPrograms(t *testing.T) {
	convey.Convey("Different frames keep their own program shape inside one batch", t, func() {
		backend := NewBackend(context.Background())

		frames := make([][128]uint64, 3)
		ptrs := make([]unsafe.Pointer, len(frames))

		for index := range frames {
			frames[index][0] = 0xAAAAAAAAAAAAAAAA
			frames[index][1] = 0xCCCCCCCCCCCCCCCC
			ptrs[index] = unsafe.Pointer(&frames[index])
		}

		installSlot(&frames[0], 0, encode32(0x1, 0, 1))
		installSlot(&frames[1], 0, encode32(0x6, 0, 1))
		installSlot(&frames[2], 0, encode32(0x7, 0, 1))

		err := backend.UniversalBitwise(ptrs)
		convey.So(err, convey.ShouldBeNil)
		convey.So(frames[0][1], convey.ShouldEqual, uint64(0x8888888888888888))
		convey.So(frames[1][1], convey.ShouldEqual, uint64(0x6666666666666666))
		convey.So(frames[2][1], convey.ShouldEqual, uint64(0xEEEEEEEEEEEEEEEE))
	})
}

func TestUniversalBitwiseSelfModifyingPrograms(t *testing.T) {
	convey.Convey("Frames that rewrite later slots diverge correctly inside one batch", t, func() {
		backend := NewBackend(context.Background())

		var xorFrame, orFrame [128]uint64
		ptrs := []unsafe.Pointer{
			unsafe.Pointer(&xorFrame),
			unsafe.Pointer(&orFrame),
		}

		xorFrame[1] = 0xAAAAAAAAAAAAAAAA
		xorFrame[2] = 0xCCCCCCCCCCCCCCCC
		xorFrame[3] = uint64(encode32(0x6, 1, 2)) << 32

		orFrame[1] = 0xAAAAAAAAAAAAAAAA
		orFrame[2] = 0xCCCCCCCCCCCCCCCC
		orFrame[3] = uint64(encode32(0x7, 1, 2)) << 32

		// Slot 0 copies word 3 into the first program word, replacing slot 1.
		installSlot(&xorFrame, 0, encode32(0x3, 3, 76))
		installSlot(&orFrame, 0, encode32(0x3, 3, 76))

		err := backend.UniversalBitwise(ptrs)
		convey.So(err, convey.ShouldBeNil)
		convey.So(xorFrame[2], convey.ShouldEqual, uint64(0x6666666666666666))
		convey.So(orFrame[2], convey.ShouldEqual, uint64(0xEEEEEEEEEEEEEEEE))
	})
}

func TestUniversalBitwiseChainedSlots(t *testing.T) {
	convey.Convey("Multiple instruction slots execute sequentially", t, func() {
		backend := NewBackend(context.Background())

		var f [128]uint64
		f[0] = 0xFF00FF00FF00FF00
		f[1] = 0x00FF00FF00FF00FF
		f[2] = 0

		// Slot 0: XOR word 0 and word 1, result in word 2
		// But XOR takes src and dst, so: word2 = TT(0x6, word0, word2)
		// That XORs word0 into word2 (which is 0), so word2 = word0.
		installSlot(&f, 0, encode32(0x6, 0, 2)) // word2 ^= word0 → word2 = word0
		// Slot 1: XOR word1 into word2: word2 = word0 ^ word1
		installSlot(&f, 1, encode32(0x6, 1, 2)) // word2 ^= word1

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
		convey.So(err, convey.ShouldBeNil)
		convey.So(f[2], convey.ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
	})
}

func TestUniversalBitwiseAffineFollowUp(t *testing.T) {
	convey.Convey("After execution, FW and Accumulator are updated for rescheduling", t, func() {
		backend := NewBackend(context.Background())

		var f [128]uint64
		f[core.Cfg.Value.Region.State.Accumulator] = 42
		f[core.Cfg.Value.Region.State.Sequence] = 7
		// Install at least one non-NOP so execution runs.
		installSlot(&f, 0, encode32(0x3, 0, 0)) // COPY A (dst=src=0, noop)

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
		convey.So(err, convey.ShouldBeNil)

		fwWord := core.Cfg.Value.Region.Registers.FW
		accWord := core.Cfg.Value.Region.State.Accumulator

		// FW should be non-zero (holographic schedule signature).
		convey.So(f[fwWord], convey.ShouldNotEqual, 0)
		// Accumulator should have evolved.
		convey.So(f[accWord], convey.ShouldNotEqual, 42)
	})
}

func TestUniversalBitwiseNilFrame(t *testing.T) {
	convey.Convey("Nil frame returns error", t, func() {
		backend := NewBackend(context.Background())
		err := backend.UniversalBitwise([]unsafe.Pointer{nil})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestUniversalBitwiseAffineUnroll(t *testing.T) {
	convey.Convey("AffineUnrollSlots produces correct program for SIMD execution", t, func() {
		backend := NewBackend(context.Background())

		var f [128]uint64
		// Fill token words 0-7 with data.
		for i := 0; i < 8; i++ {
			f[i] = 0xAAAAAAAAAAAAAAAA
		}
		// Word 8 will be the src for all ops.
		f[8] = 0x5555555555555555

		// Use AffineUnrollSlots to generate 8 AND instructions:
		// each targets dst words 0..7 with src=8, stride=1.
		// encode32 format: op=0x1(AND), src=8, dst=0..7
		baseOp := uint32(0x1) | uint32(8)<<4 // op=AND, src=8, dst will be added
		slots := firmware.AffineUnrollSlots(baseOp, 1, 8, 18, 8)

		for i, instr := range slots {
			installSlot(&f, i, instr)
		}

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&f)})
		convey.So(err, convey.ShouldBeNil)

		for i := 0; i < 8; i++ {
			convey.So(f[i], convey.ShouldEqual, uint64(0x0000000000000000))
		}
	})
}

func BenchmarkUniversalBitwise(b *testing.B) {
	backend := NewBackend(context.Background())

	batchSize := 1024
	frames := make([][128]uint64, batchSize)
	ptrs := make([]unsafe.Pointer, batchSize)

	for i := range frames {
		frames[i][0] = uint64(i)
		frames[i][1] = 0xFFFFFFFFFFFFFFFF
		installSlot(&frames[i], 0, encode32(0x6, 0, 1))
		installSlot(&frames[i], 1, encode32(0x1, 0, 1))
		ptrs[i] = unsafe.Pointer(&frames[i])
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		if err := backend.UniversalBitwise(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}
