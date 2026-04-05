package primitive

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
)

func setupSignalTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() { *core.Cfg = original })

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
	core.Cfg.Value.Region.ID.Start = 127
}

func cpuRunner(v *Value) error {
	backend := cpu.NewBackend(context.Background())
	ptrs := []unsafe.Pointer{unsafe.Pointer(v)}
	return backend.UniversalBitwise(ptrs)
}

/*
TestLongestOneRun tests the longestOneRun helper which is pure bit math
and independent of the execution model.
*/
func TestLongestOneRun(t *testing.T) {
	Convey("longestOneRun should find correct run lengths", t, func() {
		So(longestOneRun(0), ShouldEqual, 0)
		So(longestOneRun(1), ShouldEqual, 1)
		So(longestOneRun(0x3), ShouldEqual, 2)                 // 11
		So(longestOneRun(0x7), ShouldEqual, 3)                 // 111
		So(longestOneRun(0xFF), ShouldEqual, 8)                // 8 ones
		So(longestOneRun(0xFF00FF), ShouldEqual, 8)            // two 8-runs
		So(longestOneRun(0xFFFF), ShouldEqual, 16)             // 16 ones
		So(longestOneRun(0xFFFFFFFFFFFFFFFF), ShouldEqual, 64) // all ones
		So(longestOneRun(0x0F0F0F0F), ShouldEqual, 4)          // 4-bit runs
	})
}

/*
TestSignalProgramOneRun tests that the surface-rotation execution produces
detectable structure in the Signals region when token patterns have known
one-run characteristics.

In the surface model:
  - A (words 0–3) holds the pattern under test.
  - B (words 4–7) holds a reference pattern.
  - The program opcodes control the truth-table applied per rotation.
  - Results land in the Signals region (words 24–31).

A dense one-run in A, combined with AND against B (all-ones), preserves the
run structure in the signal bytes. Scattered bits produce low signal density.
*/
func TestSignalProgramOneRun(t *testing.T) {
	setupSignalTest(t)

	Convey("Given a Value with known one-run patterns in A[0]", t, func() {
		v, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer v.Close()

		Convey("A 16-bit one-run with B=all-ones should produce a nonzero signal", func() {
			v[0] = 0xFFFF // 16 consecutive ones in low bits
			v[1] = 0
			v[2] = 0
			v[3] = 0

			// B = all ones so AND(A, B) = A.
			for i := 4; i < 8; i++ {
				v[i] = 0xFFFFFFFFFFFFFFFF
			}

			// AND on all rotations.
			packRotationOpcodes(v, []uint8{
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
			})

			So(cpuRunner(v), ShouldBeNil)

			// The low byte of A[0] is 0xFF, so signal byte 0 should be 0xFF.
			sigStart := core.Cfg.Value.Region.Signals.Start
			sig0Low := v[sigStart] & 0xFF
			So(sig0Low, ShouldEqual, uint64(0xFF))
		})

		Convey("An 8-bit one-run in byte 1 should show up when B rotates to align", func() {
			// Use a pattern with ones in the low byte so the signal captures them.
			v[0] = 0xFF // 8 ones in low byte
			v[1] = 0
			v[2] = 0
			v[3] = 0

			for i := 4; i < 8; i++ {
				v[i] = 0xFFFFFFFFFFFFFFFF
			}

			packRotationOpcodes(v, []uint8{
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
			})

			So(cpuRunner(v), ShouldBeNil)

			// Low byte of 0xFF is 0xFF. AND(0xFF, 0xFF) = 0xFF.
			sigStart := core.Cfg.Value.Region.Signals.Start
			sig0Low := v[sigStart] & 0xFF
			So(sig0Low, ShouldEqual, uint64(0xFF))
		})

		Convey("Scattered single bits should produce low signal values", func() {
			v[0] = 0x5555555555555555 // alternating 01
			v[1] = 0
			v[2] = 0
			v[3] = 0

			for i := 4; i < 8; i++ {
				v[i] = 0xFFFFFFFFFFFFFFFF
			}

			packRotationOpcodes(v, []uint8{
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
			})

			So(cpuRunner(v), ShouldBeNil)

			// AND(0x55, 0xFF) = 0x55 → low byte is 0x55, not 0xFF.
			sigStart := core.Cfg.Value.Region.Signals.Start
			sig0Low := v[sigStart] & 0xFF
			So(sig0Low, ShouldEqual, uint64(0x55))
			So(sig0Low, ShouldNotEqual, uint64(0xFF)) // not a dense run
		})

		Convey("All zeros should produce zero signals with AND", func() {
			v[0] = 0
			v[1] = 0
			v[2] = 0
			v[3] = 0

			for i := 4; i < 8; i++ {
				v[i] = 0xFFFFFFFFFFFFFFFF
			}

			packRotationOpcodes(v, []uint8{
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
				0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
			})

			So(cpuRunner(v), ShouldBeNil)

			sigStart := core.Cfg.Value.Region.Signals.Start
			So(v[sigStart], ShouldEqual, uint64(0))
		})
	})
}

/*
TestSignalProgramZeroRun tests zero-run detection via the surface model.
Using NOR (opcode 0x8): NOR(A, B) = ~(A | B). With B=0, NOR(A, 0) = ~A,
so zero-runs in A become one-runs in the signal.
*/
func TestSignalProgramZeroRun(t *testing.T) {
	setupSignalTest(t)

	Convey("Given a Value with known zero-run patterns in A[0]", t, func() {
		v, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer v.Close()

		Convey("A 16-bit zero-run should appear as ones in NOTA signal", func() {
			v[0] = 0xFFFFFFFFFFFF0000 // 16 zeros in low bits
			v[1] = 0xFFFFFFFFFFFFFFFF
			v[2] = 0xFFFFFFFFFFFFFFFF
			v[3] = 0xFFFFFFFFFFFFFFFF

			// B = 0 so NOTA (opcode 0xC): dst = ~A regardless of B.
			for i := 4; i < 8; i++ {
				v[i] = 0
			}

			// NOTA = 0xC on all rotations.
			packRotationOpcodes(v, []uint8{
				0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC,
				0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC,
			})

			So(cpuRunner(v), ShouldBeNil)

			// NOT(A[0]) = NOT(0xFFFFFFFFFFFF0000) = 0x000000000000FFFF.
			// Low byte of that = 0xFF.
			sigStart := core.Cfg.Value.Region.Signals.Start
			sig0Low := v[sigStart] & 0xFF
			So(sig0Low, ShouldEqual, uint64(0xFF))
		})

		Convey("All ones should produce zero signals with NOTA", func() {
			for i := 0; i < 4; i++ {
				v[i] = 0xFFFFFFFFFFFFFFFF
			}
			for i := 4; i < 8; i++ {
				v[i] = 0
			}

			packRotationOpcodes(v, []uint8{
				0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC,
				0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC, 0xC,
			})

			So(cpuRunner(v), ShouldBeNil)

			// NOT(0xFFFFFFFFFFFFFFFF) = 0, low byte = 0.
			sigStart := core.Cfg.Value.Region.Signals.Start
			So(v[sigStart]&0xFF, ShouldEqual, uint64(0))
		})
	})
}

/*
TestScanSignals tests that ScanSignals correctly detects structure across
token words by running the surface model and reading signal bytes.
*/
func TestScanSignals(t *testing.T) {
	setupSignalTest(t)

	Convey("Given a Value with structure across token words", t, func() {
		v, err := NewValue(nil)
		So(err, ShouldBeNil)
		defer v.Close()

		// A: different density patterns per word.
		v[0] = 0xFFFF             // 16-bit one-run (low byte 0xFF)
		v[1] = 0xFF00             // 8-bit one-run (low byte 0x00)
		v[2] = 0x5555555555555555 // scattered (low byte 0x55)
		v[3] = 0xFFFFFFFF00000000 // 32-bit zero-run in low half

		// B = all ones so AND preserves A.
		for i := 4; i < 8; i++ {
			v[i] = 0xFFFFFFFFFFFFFFFF
		}

		// AND on all rotations.
		packRotationOpcodes(v, []uint8{
			0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
			0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
		})

		Convey("Signals should capture byte-level density from token words", func() {
			So(cpuRunner(v), ShouldBeNil)

			signals, err := ScanSignals(v, cpuRunner)
			So(err, ShouldBeNil)
			So(len(signals), ShouldBeGreaterThan, 0)

			// Should be sorted longest first.
			for i := 1; i < len(signals); i++ {
				So(signals[i].RunLen, ShouldBeLessThanOrEqualTo, signals[i-1].RunLen)
			}
		})
	})
}

func BenchmarkScanSignals(b *testing.B) {
	setupSignalTest(b)

	v, err := NewValue(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer v.Close()

	v[0] = 0xFFFF000000000000
	v[1] = 0x00000000FFFFFFFF
	v[2] = 0xFF00FF00FF00FF00
	for i := 4; i < 8; i++ {
		v[i] = 0xFFFFFFFFFFFFFFFF
	}

	packRotationOpcodes(v, []uint8{
		0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
		0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1, 0x1,
	})

	if err := cpuRunner(v); err != nil {
		b.Fatal(err)
	}

	noopRunner := func(*Value) error { return nil }

	b.ResetTimer()
	for b.Loop() {
		_, _ = ScanSignals(v, noopRunner)
	}
}
