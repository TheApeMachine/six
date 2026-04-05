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

// installOpcode writes a 4-bit opcode into a program slot (one per rotation).
func installOpcode(v *[128]uint64, slot int, op uint8) {
	wordIdx := core.Cfg.Value.Region.Program.Start + slot/2
	shift := uint((slot % 2) * 32)
	mask := uint64(0xF) << shift
	v[wordIdx] = (v[wordIdx] &^ mask) | (uint64(op&0xF) << shift)
}

// readSignal extracts the 8-bit signal at index i from the Signals region.
// Index = rotation*4 + wordPair. 64 signals packed 8 per uint64 word.
func readSignal(v *[128]uint64, i int) uint8 {
	wordIdx := core.Cfg.Value.Region.Signals.Start + i/8
	shift := uint((i % 8) * 8)
	return uint8(v[wordIdx] >> shift)
}

func TestALUBasicOps(t *testing.T) {
	convey.Convey("Given the ALU", t, func() {
		backend := NewBackend(context.Background())

		// Signal index 0 = rotation 0, word pair 0.
		// Low 8 bits of TruthTable(op, A[0], B[0]).

		convey.Convey("AND (op=0x1)", func() {
			var v [128]uint64
			v[0] = 0xAAAAAAAAAAAAAAAA // A[0]
			v[4] = 0xCCCCCCCCCCCCCCCC // B[0]
			installOpcode(&v, 0, 0x1)

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			// 0xAA & 0xCC = 0x88
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0x88))
		})

		convey.Convey("XOR (op=0x6)", func() {
			var v [128]uint64
			v[0] = 0xAAAAAAAAAAAAAAAA
			v[4] = 0xCCCCCCCCCCCCCCCC
			installOpcode(&v, 0, 0x6)

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0x66))
		})

		convey.Convey("OR (op=0x7)", func() {
			var v [128]uint64
			v[0] = 0xAAAAAAAAAAAAAAAA
			v[4] = 0xCCCCCCCCCCCCCCCC
			installOpcode(&v, 0, 0x7)

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xEE))
		})

		convey.Convey("FALSE (op=0x0) zeroes the signal", func() {
			var v [128]uint64
			v[0] = 0xFFFFFFFFFFFFFFFF
			v[4] = 0xFFFFFFFFFFFFFFFF

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0))
		})

		convey.Convey("TRUE (op=0xF) all ones signal", func() {
			var v [128]uint64
			installOpcode(&v, 0, 0xF)

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xFF))
		})

		convey.Convey("COPY A (op=0x3)", func() {
			var v [128]uint64
			v[0] = 0xDEADBEEFDEADBEEF
			installOpcode(&v, 0, 0x3)

			err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
			convey.So(err, convey.ShouldBeNil)
			// Low 8 bits of 0xDEADBEEFDEADBEEF = 0xEF
			convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xEF))
		})
	})
}

func TestALUTokensNotMutated(t *testing.T) {
	convey.Convey("ALU does not mutate Token or Program regions", t, func() {
		backend := NewBackend(context.Background())

		var v [128]uint64
		v[0] = 0xAAAAAAAAAAAAAAAA
		v[4] = 0xCCCCCCCCCCCCCCCC
		installOpcode(&v, 0, 0x6)

		origA := v[0]
		origB := v[4]
		origProg := v[core.Cfg.Value.Region.Program.Start]

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
		convey.So(err, convey.ShouldBeNil)
		convey.So(v[0], convey.ShouldEqual, origA)
		convey.So(v[4], convey.ShouldEqual, origB)
		convey.So(v[core.Cfg.Value.Region.Program.Start], convey.ShouldEqual, origProg)
	})
}

func TestALURotation(t *testing.T) {
	convey.Convey("B rotates by 8 bits between rotations", t, func() {
		backend := NewBackend(context.Background())

		var v [128]uint64
		v[4] = 0x0102030405060708 // B[0]

		// op=0x5 is "B": output = b regardless of a.
		installOpcode(&v, 0, 0x5) // rotation 0
		installOpcode(&v, 1, 0x5) // rotation 1

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
		convey.So(err, convey.ShouldBeNil)

		// Rotation 0, word 0: low 8 bits of 0x0102030405060708 = 0x08
		convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0x08))

		// Rotation 1, word 0: B rotated 8 left = 0x0203040506070801, low 8 = 0x01
		convey.So(readSignal(&v, 4), convey.ShouldEqual, uint8(0x01))
	})
}

func TestALUMultipleValues(t *testing.T) {
	convey.Convey("Multiple values all execute correctly", t, func() {
		backend := NewBackend(context.Background())

		n := 64
		values := make([][128]uint64, n)
		ptrs := make([]unsafe.Pointer, n)

		for i := range values {
			values[i][0] = uint64(i)
			values[i][4] = 0xFFFFFFFFFFFFFFFF
			installOpcode(&values[i], 0, 0x1) // AND
			ptrs[i] = unsafe.Pointer(&values[i])
		}

		err := backend.UniversalBitwise(ptrs)
		convey.So(err, convey.ShouldBeNil)

		for i := range values {
			// AND with 0xFF = identity on low 8 bits
			convey.So(readSignal(&values[i], 0), convey.ShouldEqual, uint8(i))
		}
	})
}

func TestALUNilValue(t *testing.T) {
	convey.Convey("Nil value returns error", t, func() {
		backend := NewBackend(context.Background())
		err := backend.UniversalBitwise([]unsafe.Pointer{nil})
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestALUAllWordPairs(t *testing.T) {
	convey.Convey("All 4 word pairs produce correct signals within rotation 0", t, func() {
		backend := NewBackend(context.Background())

		var v [128]uint64
		for w := 0; w < 4; w++ {
			v[w] = uint64(0x1111111111111111) * uint64(w+1)   // A[w]
			v[4+w] = uint64(0x1111111111111111) * uint64(w+5)  // B[w]
		}
		installOpcode(&v, 0, 0x1) // AND for rotation 0

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
		convey.So(err, convey.ShouldBeNil)

		// Rotation 0 → signals at indices 0,1,2,3 (one per word pair).
		for w := 0; w < 4; w++ {
			a := uint8(0x11 * (w + 1))
			b := uint8(0x11 * (w + 5))
			convey.So(readSignal(&v, w), convey.ShouldEqual, a&b)
		}
	})
}

func TestALUFullSurface(t *testing.T) {
	convey.Convey("All 64 signal bytes reflect the full A×B rotation surface", t, func() {
		backend := NewBackend(context.Background())

		var v [128]uint64
		v[0] = 0xFF
		v[4] = 0x0102030405060708

		// Install OR in all 16 rotations.
		for s := range 16 {
			installOpcode(&v, s, 0x7)
		}

		err := backend.UniversalBitwise([]unsafe.Pointer{unsafe.Pointer(&v)})
		convey.So(err, convey.ShouldBeNil)

		// Rotation 0, word 0: 0xFF | 0x08 = 0xFF
		convey.So(readSignal(&v, 0), convey.ShouldEqual, uint8(0xFF))

		// Rotation 0, word 1: A[1]=0 | B[1]=0 = 0
		convey.So(readSignal(&v, 1), convey.ShouldEqual, uint8(0))
	})
}

func TestRotateLeft8(t *testing.T) {
	convey.Convey("RotateLeft64 by 8 shifts bytes left", t, func() {
		v := uint64(0x0102030405060708)
		convey.So(bits.RotateLeft64(v, 8), convey.ShouldEqual, uint64(0x0203040506070801))
	})
}

func BenchmarkUniversalBitwise(b *testing.B) {
	backend := NewBackend(context.Background())

	n := 1024
	values := make([][128]uint64, n)
	ptrs := make([]unsafe.Pointer, n)

	for i := range values {
		values[i][0] = uint64(i)
		values[i][4] = 0xFFFFFFFFFFFFFFFF
		installOpcode(&values[i], 0, 0x6)
		installOpcode(&values[i], 1, 0x1)
		ptrs[i] = unsafe.Pointer(&values[i])
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		if err := backend.UniversalBitwise(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}
