package cpu

import (
	"context"
	"math/bits"
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func init() {
	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 512
	core.Cfg.Value.Region.Affinity.Start = 8
	core.Cfg.Value.Region.Affinity.Bits = 512
	core.Cfg.Value.Region.Program.Start = 16
	core.Cfg.Value.Region.Program.Bits = 512
	core.Cfg.Value.Region.Signals.Start = 24
	core.Cfg.Value.Region.Signals.Bits = 512
	core.Cfg.Value.Region.Reserved.Start = 32
	core.Cfg.Value.Region.Reserved.Bits = 5952
	core.Cfg.Value.Region.Prev.Start = 125
	core.Cfg.Value.Region.Next.Start = 126
	core.Cfg.Value.Region.ID.Start = 127
	core.Cfg.Value.Words = 128
}

/*
setupV2 prepares a Value with the V2 layout: single opcode at
word 16, A in words 0-3, and B rotation 0 (unrotated) at words
32-35. This mirrors what the programmer produces.
*/
func setupV2(v *[128]uint64, op uint8, a [4]uint64, b [4]uint64) {
	v[0], v[1], v[2], v[3] = a[0], a[1], a[2], a[3]
	v[16] = uint64(op & 0xF)

	// Write 16 rotations of B starting at word 32.
	for rot := range 16 {
		off := 32 + rot*4
		v[off] = b[0]
		v[off+1] = b[1]
		v[off+2] = b[2]
		v[off+3] = b[3]

		b[0] = bits.RotateLeft64(b[0], 8)
		b[1] = bits.RotateLeft64(b[1], 8)
		b[2] = bits.RotateLeft64(b[2], 8)
		b[3] = bits.RotateLeft64(b[3], 8)
	}
}

/*
readSignal extracts the 8-bit signal at index i from the Signals
region. Index = rotation*4 + word. 64 signals packed 8 per uint64.
*/
func readSignal(v *[128]uint64, i int) uint8 {
	wordIdx := core.Cfg.Value.Region.Signals.Start + i/8
	shift := uint((i % 8) * 8)
	return uint8(v[wordIdx] >> shift)
}

func TestExecute(t *testing.T) {
	convey.Convey("Given the ALU via Execute", t, func() {
		backend := NewBackend(context.Background())

		convey.Convey("AND (op=0x1)", func() {
			var v [128]uint64
			setupV2(&v, 0x1,
				[4]uint64{0xAAAAAAAAAAAAAAAA, 0, 0, 0},
				[4]uint64{0xCCCCCCCCCCCCCCCC, 0, 0, 0},
			)

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0x88))
		})

		convey.Convey("XOR (op=0x6)", func() {
			var v [128]uint64
			setupV2(&v, 0x6,
				[4]uint64{0xAAAAAAAAAAAAAAAA, 0, 0, 0},
				[4]uint64{0xCCCCCCCCCCCCCCCC, 0, 0, 0},
			)

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0x66))
		})

		convey.Convey("OR (op=0x7)", func() {
			var v [128]uint64
			setupV2(&v, 0x7,
				[4]uint64{0xAAAAAAAAAAAAAAAA, 0, 0, 0},
				[4]uint64{0xCCCCCCCCCCCCCCCC, 0, 0, 0},
			)

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xEE))
		})

		convey.Convey("FALSE (op=0x0) zeroes the signal", func() {
			var v [128]uint64
			setupV2(&v, 0x0,
				[4]uint64{0xFFFFFFFFFFFFFFFF, 0, 0, 0},
				[4]uint64{0xFFFFFFFFFFFFFFFF, 0, 0, 0},
			)

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0))
		})

		convey.Convey("TRUE (op=0xF) all ones signal", func() {
			var v [128]uint64
			setupV2(&v, 0xF,
				[4]uint64{0, 0, 0, 0},
				[4]uint64{0, 0, 0, 0},
			)
			// No batch marker, so this goes through truth table, not CSA.
			// But 0xF with batchCount=0 goes to default case.
			// Wait — 0xF with batchCount=0 hits the default (universalBitwiseV2).
			// The truth table for 0xF: a&b | a&~b | ~a&b | ~a&~b = 0xFF always.

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xFF))
		})

		convey.Convey("Nil frame returns error", func() {
			err := backend.Execute([]unsafe.Pointer{nil})
			convey.So(err, convey.ShouldNotBeNil)
		})

		convey.Convey("All 4 word pairs produce correct signals", func() {
			var v [128]uint64
			a := [4]uint64{
				0x1111111111111111,
				0x2222222222222222,
				0x3333333333333333,
				0x4444444444444444,
			}
			b := [4]uint64{
				0x5555555555555555,
				0x6666666666666666,
				0x7777777777777777,
				0x8888888888888888,
			}
			setupV2(&v, 0x1, a, b) // AND

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)

			for w := range 4 {
				expected := uint8(a[w]) & uint8(b[w])
				convey.So(readSignal(&v, w), convey.ShouldEqual, expected)
			}
		})

		convey.Convey("Multiple values all execute correctly", func() {
			n := 64
			values := make([][128]uint64, n)
			ptrs := make([]unsafe.Pointer, n)

			for i := range values {
				setupV2(&values[i], 0x1,
					[4]uint64{uint64(i), 0, 0, 0},
					[4]uint64{0xFFFFFFFFFFFFFFFF, 0, 0, 0},
				)
				ptrs[i] = unsafe.Pointer(&values[i])
			}

			err := backend.Execute(ptrs)
			convey.So(err, convey.ShouldBeNil)

			for i := range values {
				convey.So(readSignal(&values[i], 0), convey.ShouldEqual, uint8(i))
			}
		})
	})
}

func TestExecuteBatchDistance(t *testing.T) {
	convey.Convey("Given a batch distance frame", t, func() {
		backend := NewBackend(context.Background())

		convey.Convey("It computes Hamming distances", func() {
			var v [128]uint64

			// Query: all zeros (words 0-7).
			// Candidates at word 32, each 8 words.
			// Candidate 0: all zeros → distance 0.
			// Candidate 1: word 0 = 0xFF → distance 8.
			v[16] = 0x6       // XOR opcode
			v[124] = 2        // 2 candidates
			v[32+8+0] = 0xFF  // candidate 1, word 0

			err := backend.Execute([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)

			distances := (*[64]uint32)(unsafe.Pointer(&v[24]))
			convey.So(distances[0], convey.ShouldEqual, 0)
			convey.So(distances[1], convey.ShouldEqual, 8)
		})
	})
}

func TestRotateLeft8(t *testing.T) {
	convey.Convey("RotateLeft64 by 8 shifts bytes left", t, func() {
		v := uint64(0x0102030405060708)
		convey.So(bits.RotateLeft64(v, 8), convey.ShouldEqual, uint64(0x0203040506070801))
	})
}

func BenchmarkExecute(b *testing.B) {
	backend := NewBackend(context.Background())

	n := 1024
	values := make([][128]uint64, n)
	ptrs := make([]unsafe.Pointer, n)

	for i := range values {
		setupV2(&values[i], 0x6,
			[4]uint64{uint64(i), 0, 0, 0},
			[4]uint64{0xFFFFFFFFFFFFFFFF, 0, 0, 0},
		)
		ptrs[i] = unsafe.Pointer(&values[i])
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		if err := backend.Execute(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}
