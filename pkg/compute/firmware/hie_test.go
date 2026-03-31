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
			HolographicCrossover(&recipient, &donorA, &donorB, rng)
			So(InstructionSlot(&recipient, first), ShouldNotEqual, 0)
			before := InstructionSlot(&recipient, 0)
			HolographicCrossover(&recipient, &donorA, &donorB, rand.New(rand.NewSource(99)))
			So(InstructionSlot(&recipient, 0), ShouldEqual, before)
		})
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
		HolographicCrossover(&want, &want, &donor, rand.New(rand.NewSource(seed)))
		HolographicCrossoverTwoParent(&got, &donor, rand.New(rand.NewSource(seed)))
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
		HolographicCrossover(&recipient, &donorA, &donorB, rng)
	}
}
