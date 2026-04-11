package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParse(t *testing.T) {
	Convey("Parse should accept programs block syntax from config.yml", t, func() {
		src := `tokens[0,2] tokens[2,2] signals[0,1] xor accumulate
tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate
affinity[0,5] affinity[0,5] affinity[4,1] xor reduce
`
		toks, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(cont, ShouldBeNil)
		So(len(toks), ShouldEqual, 3)
		So(toks[0].SrcA, ShouldEqual, "tokens[0,2]")
		So(toks[0].SrcARef.Name, ShouldEqual, "tokens")
		So(toks[0].SrcARef.Start, ShouldEqual, 0)
		So(toks[0].SrcARef.Span, ShouldEqual, 2)
		So(toks[0].DstRef.Name, ShouldEqual, "signals")
		So(toks[0].DstRef.Span, ShouldEqual, 1)
		So(toks[0].ModeBit, ShouldEqual, ModeAccumulate)
		So(toks[2].ModeBit, ShouldEqual, ModeReduce)
	})

	Convey("Parse should accept comment lines and inline trailing comments", t, func() {
		src := `# leading comment
tokens[0,1] tokens[1,1] signals[0,1] xor accumulate # trailing comment
# mid comment
tokens[0,1] tokens[1,1] signals[0,1] and reduce
`
		toks, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(len(toks), ShouldEqual, 2)
	})

	Convey("Parse should accept trailing next line", t, func() {
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
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
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
next self
`
		_, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)
		So(cont.Kind, ShouldEqual, ContinuationSelf)
	})

	Convey("Parse should reject duplicate next", t, func() {
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
next 1
next 2
`
		_, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject op after next", t, func() {
		src := `next 1
tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
`
		_, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject wrong field count", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0,1] signals[0,1] xor accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject invalid region ref", t, func() {
		_, _, err := NewParser(NewProgram("badref signals[0,1] signals[0,1] xor accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject unknown op", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0,1] tokens[1,1] signals[0,1] mystery accumulate")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject unknown execution mode", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0,1] tokens[1,1] signals[0,1] xor wishful")).Parse()

		So(err, ShouldNotBeNil)
	})

	Convey("Parse should reject region ref that overflows its region", t, func() {
		_, _, err := NewParser(NewProgram("tokens[0,64] tokens[0,1] signals[0,1] xor accumulate")).Parse()

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
	src := `tokens[0,16] affinity[0,5] signals[0,1] and reduce
tokens[0,16] affinity[0,5] signals[1,1] or reduce
`
	program := NewProgram(src)

	b.ResetTimer()

	for range b.N {
		parser := NewParser(program)
		program.ResetParseState()
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
