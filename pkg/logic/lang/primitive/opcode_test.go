package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestOpcodeSetters verifies individual program-field setters.
*/
func TestOpcodeSetters(t *testing.T) {
	gc.Convey("Given a value with program metadata", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetProgram(OpcodeNext, 0, 0, false)
		value.SetJump(1234)
		value.SetBranches(5)
		value.SetTerminal(true)

		gc.So(value.Jump(), gc.ShouldEqual, uint32(1234))
		gc.So(value.Branches(), gc.ShouldEqual, uint8(5))
		gc.So(value.Terminal(), gc.ShouldBeTrue)
	})
}

/*
TestOpcodeSetProgramAndProgram verifies packed encode/decode round-trip.
*/
func TestOpcodeSetProgramAndProgram(t *testing.T) {
	gc.Convey("Given a fully packed program word", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetProgram(OpcodeBranch, 99, 2, true)
		opcode, jump, branches, terminal := value.Program()

		gc.So(opcode, gc.ShouldEqual, OpcodeBranch)
		gc.So(jump, gc.ShouldEqual, uint32(99))
		gc.So(branches, gc.ShouldEqual, uint8(2))
		gc.So(terminal, gc.ShouldBeTrue)
	})
}

/*
BenchmarkOpcodeSetProgram measures control-word packing throughput.
*/
func BenchmarkOpcodeSetProgram(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	var jump uint32 = 1

	b.ResetTimer()

	for b.Loop() {
		value.SetProgram(OpcodeJump, jump, uint8(jump%7), jump%2 == 0)
		jump++
	}
}

/*
BenchmarkOpcodeProgram measures program-word unpack throughput.
*/
func BenchmarkOpcodeProgram(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	value.SetProgram(OpcodeHalt, 777, 4, true)

	b.ResetTimer()

	for b.Loop() {
		_, _, _, _ = value.Program()
	}
}
