package programmer

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func tokenProgramLine(tok Token) string {
	return strings.Join([]string{tok.SrcA, tok.SrcB, tok.Dst, tok.Op, tok.Mode}, " ")
}

func TestToken(t *testing.T) {
	original := *core.Cfg

	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given a Token round-trip through the parser", t, func() {
		line := "tokens[0,1] tokens[1,1] signals[0,1] xor accumulate"
		first, _, err := NewParser(NewProgram(line)).Parse()

		So(err, ShouldBeNil)
		So(len(first), ShouldEqual, 1)

		again, _, err := NewParser(NewProgram(tokenProgramLine(first[0]))).Parse()

		So(err, ShouldBeNil)
		So(len(again), ShouldEqual, 1)
		So(again[0], ShouldResemble, first[0])
	})

	Convey("Given nor/reduce variant", t, func() {
		line := "tokens[0,16] tokens[0,16] affinity[0,5] nor reduce"
		first, _, err := NewParser(NewProgram(line)).Parse()

		So(err, ShouldBeNil)

		again, _, err := NewParser(NewProgram(tokenProgramLine(first[0]))).Parse()

		So(err, ShouldBeNil)
		So(again[0].Op, ShouldEqual, "nor")
		So(again[0].ModeBit, ShouldEqual, ModeReduce)
		So(again[0], ShouldResemble, first[0])
	})

	Convey("Given a Token with empty SrcB string", t, func() {
		tok := Token{
			SrcA: "tokens[0,1]",
			SrcB: "",
			Dst:  "signals[0,1]",
			Op:   "xor",
			Mode: "accumulate",
		}

		_, _, err := NewParser(NewProgram(tokenProgramLine(tok))).Parse()

		So(err, ShouldNotBeNil)
	})
}

func TestOperationType_constants(t *testing.T) {
	Convey("Given OperationType truth-table constants", t, func() {
		Convey("XOR should be 0b0110", func() {
			So(uint8(XOR), ShouldEqual, 0b0110)
		})

		Convey("FALSE should be 0 and TRUE should be 0xF", func() {
			So(uint8(FALSE), ShouldEqual, 0)
			So(uint8(TRUE), ShouldEqual, 0xF)
		})
	})
}
