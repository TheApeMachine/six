package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestToken(t *testing.T) {
	Convey("Given a Token with five fields", t, func() {
		tok := Token{
			SrcA: "tokens[0]",
			SrcB: "signals[0]",
			Dst:  "affinity[0]",
			Op:   "xor",
			Mode: "accumulate",
		}

		Convey("fields should round-trip for compiler and parser", func() {
			So(tok.SrcA, ShouldEqual, "tokens[0]")
			So(tok.SrcB, ShouldEqual, "signals[0]")
			So(tok.Dst, ShouldEqual, "affinity[0]")
			So(tok.Op, ShouldEqual, "xor")
			So(tok.Mode, ShouldEqual, "accumulate")
		})
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
