package firmware

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestEncodeHIE(t *testing.T) {
	Convey("Round-trip encode and decode restores a 32-bit instruction", t, func() {
		want := uint32(0xDEADBEEF)
		hv := EncodeHIE(want)
		got := DecodeHIE(hv)
		So(got, ShouldEqual, want)
	})
}

func TestDecodeHIE(t *testing.T) {
	Convey("Decoding a pure codebook superposition returns the instr used to build it", t, func() {
		instr := uint32(0x12345678)
		hv := EncodeHIE(instr)
		So(DecodeHIE(hv), ShouldEqual, instr)
	})
}

func TestHolographicCrossover(t *testing.T) {
	Convey("Given two donors with distinct payload instructions", t, func() {
		var recipient, donorA, donorB [128]uint64
		rng := rand.New(rand.NewSource(7))
		first := ProgramPayloadFirst32BitSlot()
		instrA := uint32(0xAAAABBBB)
		instrB := uint32(0x11112222)
		SetInstructionSlot(&donorA, first, instrA)
		SetInstructionSlot(&donorB, first, instrB)
		primitiveCopyProgramPrefix(&recipient, &donorA)

		Convey("HolographicCrossover writes a decoded child without touching bootstrap slots", func() {
			HolographicCrossover(&recipient, &donorA, &donorB, rng, 0)
			So(InstructionSlot(&recipient, first), ShouldNotEqual, 0)
			before := InstructionSlot(&recipient, 0)
			HolographicCrossover(&recipient, &donorA, &donorB, rand.New(rand.NewSource(99)), 0)
			So(InstructionSlot(&recipient, 0), ShouldEqual, before)
		})
	})
}

func TestHolographicCrossoverParentBiasMax(t *testing.T) {
	Convey("parentBias 1 collapses each slot to donor A’s instruction", t, func() {
		var recipient, donorA, donorB [128]uint64
		first := ProgramPayloadFirst32BitSlot()
		instrA := uint32(0x0F0F0F0F)
		instrB := uint32(0xF0F0F0F0)
		SetInstructionSlot(&donorA, first, instrA)
		SetInstructionSlot(&donorB, first, instrB)
		HolographicCrossover(&recipient, &donorA, &donorB, rand.New(rand.NewSource(314)), 1)
		So(InstructionSlot(&recipient, first), ShouldEqual, instrA)
	})
}

func TestHolographicCrossoverParentBiasIntermediate(t *testing.T) {
	Convey("parentBias 0.5 yields donorA hits, mixed decodes, and multiple distinct children over many trials", t, func() {
		first := ProgramPayloadFirst32BitSlot()
		instrA := uint32(0x11111111)
		instrB := uint32(0xEEEEEEEE)

		var donorA, donorB [128]uint64
		SetInstructionSlot(&donorA, first, instrA)
		SetInstructionSlot(&donorB, first, instrB)

		matchA, other := 0, 0
		distinct := make(map[uint32]struct{})
		iterations := 640

		for seed := 0; seed < iterations; seed++ {
			var recipient [128]uint64
			primitiveCopyProgramPrefix(&recipient, &donorA)
			HolographicCrossover(
				&recipient,
				&donorA,
				&donorB,
				rand.New(rand.NewSource(int64(seed)+901)),
				0.5,
			)

			got := InstructionSlot(&recipient, first)
			distinct[got] = struct{}{}
			if got == instrA {
				matchA++
			} else {
				other++
			}
		}

		So(matchA+other, ShouldEqual, iterations)
		So(matchA, ShouldBeGreaterThan, 0)
		So(matchA, ShouldBeLessThan, iterations)
		So(other, ShouldBeGreaterThan, 0)
		// Nearest-neighbor decode often avoids pure donorB; many distinct children is the contract.
		So(len(distinct), ShouldBeGreaterThan, 12)
	})
}

func TestHieNoiseThirdParent(t *testing.T) {
	Convey("Affine third-parent generation is deterministic per seed and varies by slot", t, func() {
		instrA := uint32(0x12345678)
		instrB := uint32(0x87654321)

		first := hieNoiseThirdParent(8, instrA, instrB, rand.New(rand.NewSource(77)), 0)
		repeat := hieNoiseThirdParent(8, instrA, instrB, rand.New(rand.NewSource(77)), 0)
		next := hieNoiseThirdParent(9, instrA, instrB, rand.New(rand.NewSource(77)), 0)

		So(first, ShouldEqual, repeat)
		So(first, ShouldNotEqual, next)
		So(DecodeHIE(first), ShouldNotEqual, 0)
	})
}

func TestHolographicCrossoverTwoParent(t *testing.T) {
	Convey("TwoParent matches HolographicCrossover(recipient, recipient, donor, rng)", t, func() {
		var baseline, donor, want, got [128]uint64
		slot := ProgramPayloadFirst32BitSlot()
		SetInstructionSlot(&baseline, slot, 0x12345678)
		SetInstructionSlot(&donor, slot, 0xABCDEF01)
		want = baseline
		got = baseline
		seed := int64(99)
		HolographicCrossover(&want, &want, &donor, rand.New(rand.NewSource(seed)), 0)
		HolographicCrossoverTwoParent(&got, &donor, rand.New(rand.NewSource(seed)), 0)
		So(InstructionSlot(&want, slot), ShouldEqual, InstructionSlot(&got, slot))
	})
}

func primitiveCopyProgramPrefix(dst, src *[128]uint64) {
	start := core.Cfg.Value.Region.Program.Start
	prefixWords := int(core.PayloadProgramWordOffset)
	for w := 0; w < prefixWords; w++ {
		idx := start + w
		if idx < len(dst) {
			dst[idx] = src[idx]
		}
	}
}

func BenchmarkEncodeHIE(b *testing.B) {
	var sink uint64
	instr := uint32(0xCAFEBABE)
	b.ResetTimer()
	for b.Loop() {
		sink += EncodeHIE(instr)
	}
	_ = sink
}

func BenchmarkDecodeHIE(b *testing.B) {
	var sink uint32
	hv := EncodeHIE(0xDEADC0DE)
	b.ResetTimer()
	for b.Loop() {
		sink += DecodeHIE(hv)
	}
	_ = sink
}

func BenchmarkHolographicCrossover(b *testing.B) {
	var recipient, donorA, donorB [128]uint64
	rng := rand.New(rand.NewSource(1))
	SetInstructionSlot(&donorA, ProgramPayloadFirst32BitSlot(), 0x12345678)
	SetInstructionSlot(&donorB, ProgramPayloadFirst32BitSlot(), 0x87654321)
	b.ResetTimer()
	for b.Loop() {
		HolographicCrossover(&recipient, &donorA, &donorB, rng, 0)
	}
}

func BenchmarkHieNoiseThirdParent(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	instrA := uint32(0x12345678)
	instrB := uint32(0x87654321)

	b.ReportAllocs()

	for b.Loop() {
		_ = hieNoiseThirdParent(ProgramPayloadFirst32BitSlot(), instrA, instrB, rng, 0.5)
	}
}
