package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParse(t *testing.T) {
	Convey("Parse should accept programs block syntax from config.yml", t, func() {
		src := `tokens[0,2] tokens[1,3] signals[0] xor accumulate
affinity[0] signals[0] affinity[0] xor accumulate
affinity[0] signals[0] affinity[0] popcount reduce
`
		toks, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(cont, ShouldBeNil)
		So(len(toks), ShouldEqual, 3)
		So(toks[0], ShouldResemble, Token{
			SrcA: "tokens[0,2]",
			SrcB: "tokens[1,3]",
			Dst:  "signals[0]",
			Op:   "xor",
			Mode: "accumulate",
		})
		So(toks[2].Op, ShouldEqual, "popcount")
		So(toks[2].Mode, ShouldEqual, "reduce")
	})

	Convey("Parse should accept trailing next line", t, func() {
		src := `tokens[0] tokens[1] signals[0] xor accumulate
next 42
`
		toks, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(len(toks), ShouldEqual, 1)
		So(cont, ShouldNotBeNil)
		So(cont.Kind, ShouldEqual, ContinuationValueID)
		So(cont.ValueID, ShouldEqual, 42)
	})

	Convey("Parse should accept next self", t, func() {
		src := `tokens[0] tokens[1] signals[0] xor accumulate
next self
`
		_, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(cont.Kind, ShouldEqual, ContinuationSelf)
	})

	Convey("Parse should reject duplicate next", t, func() {
		src := `tokens[0] tokens[1] signals[0] xor accumulate
next 1
next 2
`
		_, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject op after next", t, func() {
		src := `next 1
tokens[0] tokens[1] signals[0] xor accumulate
`
		_, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject wrong field count", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0] signals[0] xor accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject invalid region ref", t, func() {
		_, _, err := NewParser(NewProgram("badref signals[0] signals[0] xor accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject unknown op", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0] tokens[1] signals[0] mystery accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})
}

func TestParser_validateOperationMnemonic(t *testing.T) {
	Convey("Given a Parser", t, func() {
		parser := &Parser{}

		Convey("validateOperationMnemonic should accept truth-table ops", func() {
			So(parser.validateOperationMnemonic("xor"), ShouldBeNil)
			So(parser.validateOperationMnemonic("NAND"), ShouldBeNil)
		})

		Convey("validateOperationMnemonic should accept popcount before lowering", func() {
			So(parser.validateOperationMnemonic("popcount"), ShouldBeNil)
		})

		Convey("validateOperationMnemonic should reject unknown ops", func() {
			So(parser.validateOperationMnemonic("mystery"), ShouldNotBeNil)
		})
	})
}

func BenchmarkParse(b *testing.B) {
	src := `tokens[0] affinity[0] signals[0] and reduce
tokens[0] affinity[0] signals[0] or reduce
`
	program := NewProgram(src)

	b.ResetTimer()

	for range b.N {
		parser := NewParser(program)
		program.lineFields = nil
		_, _, _ = parser.Parse()
	}
}

func BenchmarkParser_validateOperationMnemonic(b *testing.B) {
	parser := &Parser{}

	b.ResetTimer()

	for range b.N {
		_ = parser.validateOperationMnemonic("xor")
	}
}
