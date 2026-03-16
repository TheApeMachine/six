package data

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestValueProgramMetadata(t *testing.T) {
	Convey("Given a value with threaded-code metadata", t, func() {
		value := MustNewValue()

		Convey("SetProgram should persist opcode, jump, branches, and terminal state", func() {
			value.SetProgram(OpcodeBranch, 17, 3, true)

			opcode, jump, branches, terminal := value.Program()

			So(opcode, ShouldEqual, OpcodeBranch)
			So(jump, ShouldEqual, 17)
			So(branches, ShouldEqual, 3)
			So(terminal, ShouldBeTrue)
		})

		Convey("SetOpcode should preserve existing jump metadata", func() {
			value.SetProgram(OpcodeJump, 64, 1, false)
			value.SetOpcode(uint64(OpcodeHalt))

			So(value.Opcode(), ShouldEqual, uint64(OpcodeHalt))
			So(value.Jump(), ShouldEqual, 64)
			So(value.Branches(), ShouldEqual, 1)
			So(value.Terminal(), ShouldBeFalse)
		})

		Convey("Residual carry should survive alongside program metadata", func() {
			value.SetProgram(OpcodeNext, 1, 0, false)
			value.SetResidualCarry(99)

			So(value.ResidualCarry(), ShouldEqual, 99)
			So(value.Opcode(), ShouldEqual, uint64(OpcodeNext))
			So(value.Jump(), ShouldEqual, 1)
		})
	})
}

func BenchmarkValueSetProgram(b *testing.B) {
	value := MustNewValue()

	b.ResetTimer()
	for b.Loop() {
		value.SetProgram(OpcodeJump, 256, 2, false)
	}
}
