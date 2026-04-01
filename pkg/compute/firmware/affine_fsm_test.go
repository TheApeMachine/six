package firmware

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestAffineUnrollSlots(t *testing.T) {
	Convey("AffineUnrollSlots stretches indices by stride mod maxWord", t, func() {
		s := AffineUnrollSlots(0x100, 3, 8, 4, 4)
		So(len(s), ShouldEqual, 4)
		So(s[0], ShouldEqual, uint32(0x100|0))
		So(s[1], ShouldEqual, uint32(0x100|(3<<4)))
		So(s[2], ShouldEqual, uint32(0x100|(6<<4)))
		So(s[3], ShouldEqual, uint32(0x100|(1<<4)))
	})
}

func TestApplyAffineUnrollToProgram(t *testing.T) {
	Convey("ApplyAffineUnrollToProgram writes consecutive SetInstructionSlot payloads", t, func() {
		var c [128]uint64
		start := ProgramPayloadFirst32BitSlot()
		ApplyAffineUnrollToProgram(&c, start, 0xAB, 1, 4, 0, 3)
		So(InstructionSlot(&c, start), ShouldEqual, 0xAB)
		So(InstructionSlot(&c, start+1), ShouldEqual, 0xAB|1)
		So(InstructionSlot(&c, start+2), ShouldEqual, 0xAB|2)
	})
}

func TestAffineNextProgramID(t *testing.T) {
	Convey("AffineNextProgramID applies modular affine mix", t, func() {
		got := AffineNextProgramID(7, 5, 3, 11)
		So(got, ShouldEqual, (5*7+3)%11)
	})
}

func TestHolographicScheduleSignature(t *testing.T) {
	Convey("HolographicScheduleSignature XOR-mixes id with score and stride", t, func() {
		base := uint64(0x1234)
		const golden = uint64(0x9E3779B97F4A7C15)
		stride := uint64(17)
		expected := base ^ (math.Float64bits(0.75) >> 1) ^ (stride * golden)
		got := HolographicScheduleSignature(base, 0.75, 17)
		So(got, ShouldEqual, expected)
		got2 := HolographicScheduleSignature(base, 0.75, 17)
		So(got2, ShouldEqual, got)
	})
}

func TestNOPShatterLGP(t *testing.T) {
	Convey("NOPShatterLGP with multiplier 1 is identity on the payload window", t, func() {
		var c [128]uint64
		first := ProgramPayloadFirst32BitSlot()
		SetInstructionSlot(&c, first, 0x111)
		SetInstructionSlot(&c, first+1, 0x222)
		SetInstructionSlot(&c, first+2, 0x333)

		NOPShatterLGP(&c, 1, 64)

		So(InstructionSlot(&c, first), ShouldEqual, uint32(0x111))
		So(InstructionSlot(&c, first+1), ShouldEqual, uint32(0x222))
		So(InstructionSlot(&c, first+2), ShouldEqual, uint32(0x333))
	})
}

func TestPayloadLGPSpan(t *testing.T) {
	Convey("PayloadLGPSpan matches program region window", t, func() {
		first, last := PayloadLGPSpan()
		So(first, ShouldEqual, ProgramPayloadFirst32BitSlot())
		So(last, ShouldBeGreaterThan, first)
	})
}

func TestAffinePipelineWordCount(t *testing.T) {
	Convey("AffinePipelineWordCount is consistent with config", t, func() {
		n := AffinePipelineWordCount()
		So(n, ShouldBeGreaterThan, 0)
		want := (int(core.Cfg.Value.Region.Program.Bits) + 63) / 64
		So(n, ShouldEqual, want)
	})
}

func BenchmarkAffineUnrollSlots(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_ = AffineUnrollSlots(0x10, 7, 128, 8, 64)
	}
}
