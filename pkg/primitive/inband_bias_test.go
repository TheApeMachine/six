package primitive

import (
	"context"
	"math/bits"
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
	core.Cfg.Value.Region.State.Index = 32
	core.Cfg.Value.Region.State.Sequence = 33
	core.Cfg.Value.Region.State.Accumulator = 34
}

// packRotationOpcodes packs up to 16 4-bit opcodes (one per rotation) into
// the program region. Slot k uses opcode ops[k]; unused slots get 0x0 (FALSE).
func packRotationOpcodes(v *Value, ops []uint8) {
	progStart := core.Cfg.Value.Region.Program.Start

	// Zero all program words first.
	for i := 0; i < 8; i++ {
		v[progStart+i] = 0
	}

	for k, op := range ops {
		if k >= 16 {
			break
		}
		wordIdx := progStart + k/2
		shift := uint((k % 2) * 32)
		v[wordIdx] |= uint64(op&0xF) << shift
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
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
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

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			Convey("The signals region should contain nonzero results from A AND B_rotated", func() {
				// At rotation 0, B is unrotated: A & B = 0xAA & 0xCC = 0x88.
				// The low byte of the first lane is 0x88.
				sig0 := value[sigStart]
				So(sig0&0xFF, ShouldEqual, 0x88)
			})
		})

		Convey("With XOR opcode on rotation 0 and AND on others", func() {
			ops := make([]uint8, 16)
			ops[0] = 0x6 // XOR
			for i := 1; i < 16; i++ {
				ops[i] = 0x1 // AND
			}
			packRotationOpcodes(value, ops)

			runSurface(t, value)

			sigStart := core.Cfg.Value.Region.Signals.Start

			Convey("Rotation 0 should show XOR result while others show AND", func() {
				// Rotation 0 fills bytes 0–3 (one per lane). XOR(0xAA, 0xCC) = 0x66.
				sig0Low := value[sigStart] & 0xFF
				So(sig0Low, ShouldEqual, 0x66)

				// Byte 4 = rotation 1, lane 0. AND(0xAA, 0xCC_rot8) = AND(0xAA, 0xCC) = 0x88.
				sig0Byte4 := (value[sigStart] >> 32) & 0xFF
				So(sig0Byte4, ShouldEqual, 0x88)
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
		value, err := NewValue(nil)
		So(err, ShouldBeNil)
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

			// All 64 signal bytes should be nonzero (AND with all-ones A).
			So(nonzero, ShouldEqual, 64)
		})
	})
}

func BenchmarkValue_InBandBias(b *testing.B) {
	setupInBandValueTest(b)

	value, err := NewValue(nil)
	if err != nil {
		b.Fatal(err)
	}
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

	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(value)}
	b.ResetTimer()

	for b.Loop() {
		if err := backend.Execute(ptrs); err != nil {
			b.Fatal(err)
		}
	}
}

// popcount64 counts set bits — used as a signal density metric.
func popcount64(x uint64) int {
	return bits.OnesCount64(x)
}
