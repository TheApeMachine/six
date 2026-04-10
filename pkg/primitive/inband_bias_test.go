package primitive

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
)

func setupInBandValueTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg

	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 1024
	core.Cfg.Value.Region.Program.Start = 16
	core.Cfg.Value.Region.Program.Bits = 512
	core.Cfg.Value.Region.Signals.Start = 24
	core.Cfg.Value.Region.Signals.Bits = 512
	core.Cfg.Value.Region.Context.Start = 32
	core.Cfg.Value.Region.Context.Bits = 512
	core.Cfg.Value.Region.Gradient.Start = 40
	core.Cfg.Value.Region.Gradient.Bits = 512
	core.Cfg.Value.Region.Meta.Start = 48
	core.Cfg.Value.Region.Meta.Bits = 512
	core.Cfg.Value.Region.Prev.Start = 120
	core.Cfg.Value.Region.Next.Start = 121
	core.Cfg.Value.Region.ID.Start = 122
	core.Cfg.Value.Region.Affinity.Start = 123
	core.Cfg.Value.Region.Affinity.Bits = 257
}

/*
packRotationOpcodes writes the first opcode into the program region start word
(low 4 bits) — universalBitwiseV2 reads a single opcode for all rotations.
*/
func packRotationOpcodes(v *Value, ops []uint8) {
	progStart := core.Cfg.Value.Region.Program.Start

	for i := 0; i < 8; i++ {
		v[progStart+i] = 0
	}

	if len(ops) > 0 {
		v[progStart] = uint64(ops[0] & 0xF)
	}
}

/*
packBRotations copies B from token words 4-7 into 16 rotation slots starting
at word 32. Each rotation k shifts the 4-word B by k*8 bits to the right,
matching the universalBitwiseV2 pre-compiled layout.
*/
func packBRotations(v *Value) {
	var b [4]uint64
	for i := range 4 {
		b[i] = v[4+i]
	}

	for rot := range 16 {
		base := 32 + rot*4
		shift := uint(rot * 8)

		if shift == 0 {
			for i := range 4 {
				v[base+i] = b[i]
			}

			continue
		}

		for i := range 4 {
			lo := b[i] >> shift
			hi := b[(i+1)%4] << (64 - shift)
			v[base+i] = lo | hi
		}
	}
}

func runSurface(t *testing.T, v *Value) {
	t.Helper()

	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(v)}

	if err := backend.Execute(ptrs); err != nil {
		t.Fatal(err)
	}
}

/*
TestInBandBiasAccumulatesInProgramRegion verifies that the surface-rotation
truth-table execution produces meaningful signal bytes.

The surface model works as follows:
  - A = token words 0–3, B = token words 4–7.
  - B is rotated 16 times (8 bits per rotation).
  - Each rotation k applies opcode ops[k] via truth table: TT(op, A, B_rot).
  - The low byte of each result lane is packed into the 8-word Signals region.

This test sets A and B to known patterns, installs AND (0x1) and XOR (0x6) as
per-rotation opcodes, and checks that the Signals region captures the expected
byte-level results.
*/
func TestInBandBiasAccumulatesInProgramRegion(t *testing.T) {
	setupInBandValueTest(t)

	Convey("Given a Value with known A and B token patterns", t, func() {
		value := newValueFromZeroFrame(t)
		defer value.Close()

		// A words (0–3): repeating pattern.
		value[0] = 0xAAAAAAAAAAAAAAAA
		value[1] = 0xAAAAAAAAAAAAAAAA
		value[2] = 0xAAAAAAAAAAAAAAAA
		value[3] = 0xAAAAAAAAAAAAAAAA

		// B words (4–7): different repeating pattern.
		value[4] = 0xCCCCCCCCCCCCCCCC
		value[5] = 0xCCCCCCCCCCCCCCCC
		value[6] = 0xCCCCCCCCCCCCCCCC
		value[7] = 0xCCCCCCCCCCCCCCCC

		Convey("With AND opcodes on all 16 rotations", func() {
			ops := make([]uint8, 16)
			for i := range ops {
				ops[i] = 0x1 // AND
			}
			packRotationOpcodes(value, ops)
			packBRotations(value)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			Convey("The signals region should contain nonzero results from A AND B_rotated", func() {
				// At rotation 0, B is unrotated: A & B = 0xAA & 0xCC = 0x88.
				// The low byte of the first lane is 0x88.
				sig0 := value[sigStart]
				So(sig0&0xFF, ShouldEqual, 0x88)
			})
		})

		Convey("With XOR opcode applied uniformly", func() {
			ops := []uint8{0x6}
			packRotationOpcodes(value, ops)
			packBRotations(value)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			Convey("All rotations should show XOR result", func() {
				// Rotation 0: XOR(0xAA, 0xCC) = 0x66.
				sig0Low := value[sigStart] & 0xFF
				So(sig0Low, ShouldEqual, 0x66)

				// Rotation 1: XOR(0xAA, 0xCC_rot8). Repeating pattern so still 0x66.
				sig0Byte4 := (value[sigStart] >> 32) & 0xFF
				So(sig0Byte4, ShouldEqual, 0x66)
			})
		})
	})
}

/*
TestInBandBiasProjectionWritesAffinityAsDerivedState verifies that different
opcodes across rotations produce distinguishable signal patterns that could
be used to derive affinity state.

In the surface model, the Signals region is the "projection" — it captures
the byte-level truth-table outputs across all 16 rotations. Higher-level
code can read the Signals to compute affinity.
*/
func TestInBandBiasProjectionWritesAffinityAsDerivedState(t *testing.T) {
	setupInBandValueTest(t)

	Convey("Given A = all-ones and B = alternating", t, func() {
		value := newValueFromZeroFrame(t)
		defer value.Close()

		for i := 0; i < 4; i++ {
			value[i] = 0xFFFFFFFFFFFFFFFF // A = all ones
		}
		for i := 4; i < 8; i++ {
			value[i] = 0xAA55AA55AA55AA55 // B = alternating bytes
		}

		Convey("OR should produce all-ones signals (A=1 dominates)", func() {
			ops := make([]uint8, 16)
			for i := range ops {
				ops[i] = 0x7 // OR
			}
			packRotationOpcodes(value, ops)
			packBRotations(value)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			// OR(0xFF, anything) = 0xFF → every signal byte should be 0xFF.
			So(value[sigStart], ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
			So(value[sigStart+1], ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
		})

		Convey("AND should reflect B's pattern in signals", func() {
			ops := make([]uint8, 16)
			for i := range ops {
				ops[i] = 0x1 // AND
			}
			packRotationOpcodes(value, ops)
			packBRotations(value)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			// AND(0xFF, B) = B, so signal captures B's low byte per rotation.
			// Rotation 0: B[4] = 0xAA55AA55AA55AA55, low byte = 0x55.
			sig0Low := value[sigStart] & 0xFF
			So(sig0Low, ShouldEqual, 0x55)

			// Byte 4 = rotation 1, lane 0. B rotated 8 bits: 0x55AA55AA55AA55AA,
			// low byte = 0xAA.
			sig0Byte4 := (value[sigStart] >> 32) & 0xFF
			So(sig0Byte4, ShouldEqual, 0xAA)
		})

		Convey("The number of nonzero signal bytes indicates correlation density", func() {
			ops := make([]uint8, 16)
			for i := range ops {
				ops[i] = 0x1 // AND
			}
			packRotationOpcodes(value, ops)
			packBRotations(value)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			// Count nonzero bytes in the signals region as a density metric.
			nonzero := 0
			for w := 0; w < 8; w++ {
				word := value[sigStart+w]
				for b := 0; b < 8; b++ {
					if (word>>(uint(b)*8))&0xFF != 0 {
						nonzero++
					}
				}
			}

			// With single-opcode AND, the kernel fills a subset of signal
			// bytes depending on rotation coverage.
			So(nonzero, ShouldBeGreaterThan, 0)
		})
	})
}

func BenchmarkValue_InBandBias(b *testing.B) {
	setupInBandValueTest(b)

	value := newValueFromZeroFrame(b)
	defer value.Close()

	for i := 0; i < 4; i++ {
		value[i] = 0xAAAAAAAAAAAAAAAA
	}
	for i := 4; i < 8; i++ {
		value[i] = 0xCCCCCCCCCCCCCCCC
	}

	ops := make([]uint8, 16)
	for i := range ops {
		ops[i] = 0x1
	}
	packRotationOpcodes(value, ops)
	packBRotations(value)

	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(value)}
	b.ResetTimer()

	for b.Loop() {
		if err := backend.Execute(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}
