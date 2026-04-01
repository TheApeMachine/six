package cpu

import (
	"context"
	"runtime"
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestUniversalBitwise(t *testing.T) {
	convey.Convey("Given a CPU Kernel backend", t, func() {
		backend := NewBackend(context.Background())

		convey.Convey("When testing the raw SIMD execWordBlock operations", func() {
			dst := make([]uint64, 4)
			src := make([]uint64, 4)

			seed := func() {
				for i := 0; i < 4; i++ {
					dst[i] = 0xAAAAAAAAAAAAAAAA // 10101010...
					src[i] = 0xCCCCCCCCCCCCCCCC // 11001100...
				}
			}
			seed()

			convey.Convey("It should correctly apply AND (0x1)", func() {
				seed()
				execWordBlock(dst, src, 0x1)
				for i := 0; i < 4; i++ {
					convey.So(dst[i], convey.ShouldEqual, uint64(0x8888888888888888))
				}
			})

			convey.Convey("It should correctly apply COPY (0x3)", func() {
				seed()
				execWordBlock(dst, src, 0x3)
				for i := 0; i < 4; i++ {
					convey.So(dst[i], convey.ShouldEqual, uint64(0xCCCCCCCCCCCCCCCC))
				}
			})

			convey.Convey("It should correctly apply XOR (0x6)", func() {
				seed()
				execWordBlock(dst, src, 0x6)
				for i := 0; i < 4; i++ {
					// 1010 ^ 1100 = 0110
					convey.So(dst[i], convey.ShouldEqual, uint64(0x6666666666666666))
				}
			})

			convey.Convey("It should correctly apply OR (0x7)", func() {
				seed()
				execWordBlock(dst, src, 0x7)
				for i := 0; i < 4; i++ {
					// 1010 | 1100 = 1110
					convey.So(dst[i], convey.ShouldEqual, uint64(0xEEEEEEEEEEEEEEEE))
				}
			})

			convey.Convey("It should correctly apply src &^ dst (0x2)", func() {
				seed()
				// src &^ dst
				execWordBlock(dst, src, 0x2)
				for i := 0; i < 4; i++ {
					convey.So(dst[i], convey.ShouldEqual, uint64(0x4444444444444444))
				}
			})
		})

		convey.Convey("When testing the UniversalBitwise VM engine (execution loop)", func() {
			valA := new([128]uint64)
			valB := new([128]uint64)

			core.Cfg.Value.Region.Program.Start = 76
			pi := core.Cfg.Value.Region.Program.Start

			// Instruction encoding helpers matching the 16-bit RISC spec in the backend
			encodeMem := func(dir, reg, ctx, word uint16) uint16 {
				return (1 << 14) | (dir << 13) | (reg << 11) | (ctx << 10) | word
			}
			encodeAlu := func(op, reg, ctx, word uint16) uint16 {
				return (2 << 14) | (op << 10) | (reg << 8) | (ctx << 7) | word
			}
			encodeCtl := func(sub, reg uint16) uint16 {
				return (3 << 14) | (sub << 12) | (reg << 10)
			}
			_ = encodeCtl // to avoid unused warning if omitted below

			prog := []uint16{
				encodeMem(0, 0, 1, 0),   // LOAD r0 = valB[0]
				encodeAlu(0x6, 0, 0, 0), // ALU XOR: valA[0] = r0 ^ valA[0]
				0x0000,                  // HALT
			}

			// Pack program into valA's program index
			for i, instr := range prog {
				w := pi + i/4
				shift := (i % 4) * 16
				valA[w] |= uint64(instr) << shift
			}

			// Seed data words at index 0 for both contexts
			valA[0] = 0xAAAAAAAAAAAAAAAA
			valB[0] = 0xCCCCCCCCCCCCCCCC

			err := backend.UniversalBitwise(unsafe.Pointer(valA), unsafe.Pointer(valB), 1)
			convey.So(err, convey.ShouldBeNil)

			convey.Convey("It should execute the RISC instructions and compute XOR manually", func() {
				// We expect valA[0] to be overwritten with the XOR of 0xAAAA... and 0xCCCC...
				convey.So(valA[0], convey.ShouldEqual, uint64(0x6666666666666666))
			})
		})

		convey.Convey("When batching with GOMAXPROCS=1, It should match per-frame execution", func() {
			saved := runtime.GOMAXPROCS(0)
			runtime.GOMAXPROCS(1)
			defer runtime.GOMAXPROCS(saved)

			core.Cfg.Value.Region.Program.Start = 76
			pi := core.Cfg.Value.Region.Program.Start
			n := 48
			aWant := make([]uint64, 128*n)
			bWant := make([]uint64, 128*n)
			aBatch := make([]uint64, 128*n)
			bBatch := make([]uint64, 128*n)

			encodeMem := func(dir, reg, ctx, word uint16) uint16 {
				return (1 << 14) | (dir << 13) | (reg << 11) | (ctx << 10) | word
			}
			encodeAlu := func(op, reg, ctx, word uint16) uint16 {
				return (2 << 14) | (op << 10) | (reg << 8) | (ctx << 7) | word
			}

			prog := []uint16{
				encodeMem(0, 0, 1, 0),
				encodeAlu(0x6, 0, 0, 0),
				encodeMem(0, 1, 1, 32),
				encodeAlu(0x1, 1, 0, 32),
				0x0000,
			}

			reg0 := core.Cfg.Value.Region.Registers.Start
			prog0 := core.Cfg.Value.Region.Program.Start

			seed := func(aBuf, bBuf []uint64) {
				for i := 0; i < n; i++ {
					offset := i * 128
					for j := range 128 {
						aBuf[offset+j] = uint64(i + j)
						bBuf[offset+j] = uint64((i + j) * 3)
					}
					for w := reg0; w < prog0 && w < 128; w++ {
						aBuf[offset+w] = 0
						bBuf[offset+w] = 0
					}
					for k, instr := range prog {
						w := offset + int(pi) + k/4
						shift := (k % 4) * 16
						aBuf[w] |= uint64(instr) << shift
					}
				}
			}

			seed(aWant, bWant)
			seed(aBatch, bBatch)

			oneByOne := NewBackend(context.Background())
			stride := uintptr(primitive.ByteSize)
			baseA := uintptr(unsafe.Pointer(&aWant[0]))
			baseB := uintptr(unsafe.Pointer(&bWant[0]))
			for i := 0; i < n; i++ {
				off := stride * uintptr(i)
				convey.So(oneByOne.UniversalBitwise(
					unsafe.Pointer(baseA+off),
					unsafe.Pointer(baseB+off),
					1,
				), convey.ShouldBeNil)
			}

			batched := NewBackend(context.Background())
			convey.So(batched.UniversalBitwise(unsafe.Pointer(&aBatch[0]), unsafe.Pointer(&bBatch[0]), n), convey.ShouldBeNil)

			convey.So(aBatch, convey.ShouldResemble, aWant)
		})

		convey.Convey("When batching multi-core, It should match per-frame execution", func() {
			if runtime.GOMAXPROCS(0) < 2 {
				return
			}

			core.Cfg.Value.Region.Program.Start = 76
			pi := core.Cfg.Value.Region.Program.Start
			n := 48
			aWant := make([]uint64, 128*n)
			bWant := make([]uint64, 128*n)
			aBatch := make([]uint64, 128*n)
			bBatch := make([]uint64, 128*n)

			encodeMem := func(dir, reg, ctx, word uint16) uint16 {
				return (1 << 14) | (dir << 13) | (reg << 11) | (ctx << 10) | word
			}
			encodeAlu := func(op, reg, ctx, word uint16) uint16 {
				return (2 << 14) | (op << 10) | (reg << 8) | (ctx << 7) | word
			}

			prog := []uint16{
				encodeMem(0, 0, 1, 0),
				encodeAlu(0x6, 0, 0, 0),
				encodeMem(0, 1, 1, 32),
				encodeAlu(0x1, 1, 0, 32),
				0x0000,
			}

			reg0 := core.Cfg.Value.Region.Registers.Start
			prog0 := core.Cfg.Value.Region.Program.Start

			seed := func(aBuf, bBuf []uint64) {
				for i := 0; i < n; i++ {
					offset := i * 128
					for j := range 128 {
						aBuf[offset+j] = uint64(i + j)
						bBuf[offset+j] = uint64((i + j) * 3)
					}
					for w := reg0; w < prog0 && w < 128; w++ {
						aBuf[offset+w] = 0
						bBuf[offset+w] = 0
					}
					for k, instr := range prog {
						w := offset + int(pi) + k/4
						shift := (k % 4) * 16
						aBuf[w] |= uint64(instr) << shift
					}
				}
			}

			seed(aWant, bWant)
			seed(aBatch, bBatch)

			oneByOne := NewBackend(context.Background())
			stride := uintptr(primitive.ByteSize)
			baseA := uintptr(unsafe.Pointer(&aWant[0]))
			baseB := uintptr(unsafe.Pointer(&bWant[0]))
			for i := 0; i < n; i++ {
				off := stride * uintptr(i)
				convey.So(oneByOne.UniversalBitwise(
					unsafe.Pointer(baseA+off),
					unsafe.Pointer(baseB+off),
					1,
				), convey.ShouldBeNil)
			}

			batched := NewBackend(context.Background())
			convey.So(batched.UniversalBitwise(unsafe.Pointer(&aBatch[0]), unsafe.Pointer(&bBatch[0]), n), convey.ShouldBeNil)

			convey.So(aBatch, convey.ShouldResemble, aWant)
		})
	})
}

func BenchmarkUniversalBitwise(b *testing.B) {
	core.Cfg.Value.Region.Program.Start = 76
	backend := NewBackend(context.Background())

	// We create a large batch simulating typical usage
	batchSize := 1024

	// Allocate strictly 1024 bytes per Value
	aData := make([]uint64, 128*batchSize)
	bData := make([]uint64, 128*batchSize)

	encodeMem := func(dir, reg, ctx, word uint16) uint16 {
		return (1 << 14) | (dir << 13) | (reg << 11) | (ctx << 10) | word
	}
	encodeAlu := func(op, reg, ctx, word uint16) uint16 {
		return (2 << 14) | (op << 10) | (reg << 8) | (ctx << 7) | word
	}

	// This is a minimal, hot loop representative VM payload
	// 5 instructions that do 2 massive AVX block loads and XOR execution.
	prog := []uint16{
		encodeMem(0, 0, 1, 0),    // LOAD r0 = valB[0]
		encodeAlu(0x6, 0, 0, 0),  // ALU XOR: valA[0] = r0 ^ valA[0]
		encodeMem(0, 1, 1, 32),   // LOAD r1 = valB[32]
		encodeAlu(0x1, 1, 0, 32), // ALU AND: valA[32] = r1 & valA[32]
		0x0000,                   // HALT
	}

	pi := core.Cfg.Value.Region.Program.Start

	// Inject the payload into every item in the batch
	for i := 0; i < batchSize; i++ {
		offset := i * 128
		for j := range 128 {
			aData[offset+j] = uint64(i + j) // salt the data
			bData[offset+j] = uint64((i + j) * 3)
		}

		for k, instr := range prog {
			w := offset + int(pi) + k/4
			shift := (k % 4) * 16
			aData[w] |= uint64(instr) << shift
		}
	}

	aPtr := unsafe.Pointer(&aData[0])
	bPtr := unsafe.Pointer(&bData[0])

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		err := backend.UniversalBitwise(aPtr, bPtr, batchSize)
		if err != nil {
			b.Fatal(err)
		}
	}
}
