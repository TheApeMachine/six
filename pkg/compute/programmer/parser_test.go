package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewParser checks parser wiring to a Program instance.
*/
func TestNewParser(t *testing.T) {
	Convey("Given a program with one operation line", t, func() {
		source := "tokens tokens signals xor accumulate\n"
		program := NewProgram(source)
		parser := NewParser(program)

		Convey("NewParser should keep the program reference", func() {
			So(parser, ShouldNotBeNil)
			So(parser.program, ShouldEqual, program)
		})
	})
}

/*
TestParser_Parse maps five fields per line onto Token fields using RegionNames.
*/
func TestParser_Parse(t *testing.T) {
	Convey("Given a program with a xor accumulate line", t, func() {
		source := "tokens tokens signals xor accumulate\n"
		program := NewProgram(source)
		parser := NewParser(program)

		Convey("Parse should yield one token with matching regions and op", func() {
			tokens := parser.Parse()

			So(len(tokens), ShouldEqual, 1)
			So(tokens[0].SrcA.Region, ShouldEqual, primitive.TokenRegion)
			So(tokens[0].SrcB.Region, ShouldEqual, primitive.TokenRegion)
			So(tokens[0].Dst.Region, ShouldEqual, primitive.SignalsRegion)
			So(tokens[0].Op, ShouldEqual, XOR)
			So(tokens[0].Mode, ShouldEqual, ModeAccumulate)
		})
	})

	Convey("Given a program with region-local word spans", t, func() {
		source := "tokens[2,4] affinity[1,2] signals[3] xor reduce\n"
		program := NewProgram(source)
		parser := NewParser(program)

		Convey("Parse should resolve them to absolute frame spans", func() {
			tokens := parser.Parse()

			tokenStart, _ := primitive.TokenRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			signalsStart, _ := primitive.SignalsRegion.WordExtent()

			So(parser.Err(), ShouldBeNil)
			So(len(tokens), ShouldEqual, 1)
			So(tokens[0].SrcA.Start, ShouldEqual, tokenStart+2)
			So(tokens[0].SrcA.Span, ShouldEqual, 4)
			So(tokens[0].SrcB.Start, ShouldEqual, affinityStart+1)
			So(tokens[0].SrcB.Span, ShouldEqual, 2)
			So(tokens[0].Dst.Start, ShouldEqual, signalsStart+3)
			So(tokens[0].Dst.Span, ShouldEqual, 1)
		})
	})

	Convey("Given a program with a comment line", t, func() {
		source := "# header comment\ncontext context gradient or reduce\n"
		program := NewProgram(source)
		parser := NewParser(program)

		Convey("Parse should skip comment lines", func() {
			tokens := parser.Parse()

			So(len(tokens), ShouldEqual, 1)
			So(tokens[0].Op, ShouldEqual, OR)
			So(tokens[0].Mode, ShouldEqual, ModeReduce)
		})
	})

	Convey("Given a program with a trailing next self directive", t, func() {
		source := "tokens tokens signals xor accumulate\n" +
			"context context gradient xor accumulate\n" +
			"next self\n"
		program := NewProgram(source)
		parser := NewParser(program)

		Convey("Parse should emit only ALU rows and skip the scheduler line", func() {
			tokens := parser.Parse()

			So(len(tokens), ShouldEqual, 2)
			So(tokens[0].Op, ShouldEqual, XOR)
			So(tokens[1].Op, ShouldEqual, XOR)
		})
	})
}

/*
TestParser_validateOperationMnemonic gates allowed surface op spellings.
*/
func TestParser_validateOperationMnemonic(t *testing.T) {
	Convey("Given a Parser receiver", t, func() {
		var parser Parser

		Convey("validateOperationMnemonic should accept xor", func() {
			So(parser.validateOperationMnemonic("xor"), ShouldBeNil)
		})

		Convey("validateOperationMnemonic should reject unknown ops", func() {
			err := parser.validateOperationMnemonic("not_a_real_op")

			So(err, ShouldNotBeNil)
		})
	})
}

/*
TestParser_parseOperationType maps keywords to OperationType values.
*/
func TestParser_parseOperationType(t *testing.T) {
	Convey("Given a Parser receiver", t, func() {
		var parser Parser

		Convey("parseOperationType should map xor to XOR", func() {
			So(parser.parseOperationType("xor"), ShouldEqual, XOR)
		})

		Convey("parseOperationType should default unknown ops to FALSE", func() {
			So(parser.parseOperationType("typo"), ShouldEqual, FALSE)
		})
	})
}

/*
TestParser_parseExecutionMode maps mode keywords onto ExecutionMode.
*/
func TestParser_parseExecutionMode(t *testing.T) {
	Convey("Given a Parser receiver", t, func() {
		var parser Parser

		Convey("parseExecutionMode should map accumulate correctly", func() {
			So(parser.parseExecutionMode("accumulate"), ShouldEqual, ModeAccumulate)
		})

		Convey("parseExecutionMode should map reduce correctly", func() {
			So(parser.parseExecutionMode("reduce"), ShouldEqual, ModeReduce)
		})

		Convey("parseExecutionMode should default unknown modes to accumulate", func() {
			So(parser.parseExecutionMode("unknown"), ShouldEqual, ModeAccumulate)
		})
	})
}

func BenchmarkParser_Parse(b *testing.B) {
	source := "tokens tokens signals xor accumulate\ncontext context gradient or reduce\n"
	program := NewProgram(source)
	parser := NewParser(program)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = parser.Parse()
	}
}

func BenchmarkParser_parseOperationType(b *testing.B) {
	var parser Parser

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = parser.parseOperationType("xor")
	}
}
