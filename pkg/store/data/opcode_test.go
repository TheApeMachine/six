package data

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestChordProgramMetadata(t *testing.T) {
	Convey("Given a chord with threaded-code metadata", t, func() {
		chord := MustNewChord()

		Convey("SetProgram should persist opcode, jump, branches, and terminal state", func() {
			chord.SetProgram(OpcodeBranch, 17, 3, true)

			opcode, jump, branches, terminal := chord.Program()

			So(opcode, ShouldEqual, OpcodeBranch)
			So(jump, ShouldEqual, 17)
			So(branches, ShouldEqual, 3)
			So(terminal, ShouldBeTrue)
		})

		Convey("SetOpcode should preserve existing jump metadata", func() {
			chord.SetProgram(OpcodeJump, 64, 1, false)
			chord.SetOpcode(uint64(OpcodeHalt))

			So(chord.Opcode(), ShouldEqual, uint64(OpcodeHalt))
			So(chord.Jump(), ShouldEqual, 64)
			So(chord.Branches(), ShouldEqual, 1)
			So(chord.Terminal(), ShouldBeFalse)
		})

		Convey("Residual carry should survive alongside program metadata", func() {
			chord.SetProgram(OpcodeNext, 1, 0, false)
			chord.SetResidualCarry(99)

			So(chord.ResidualCarry(), ShouldEqual, 99)
			So(chord.Opcode(), ShouldEqual, uint64(OpcodeNext))
			So(chord.Jump(), ShouldEqual, 1)
		})
	})
}

func BenchmarkChordSetProgram(b *testing.B) {
	chord := MustNewChord()

	b.ResetTimer()
	for b.Loop() {
		chord.SetProgram(OpcodeJump, 256, 2, false)
	}
}
