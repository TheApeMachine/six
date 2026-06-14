package program

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestEncodeProgramIR(t *testing.T) {
	Convey("Given an IR program with a geometric slot", t, func() {
		ir, err := EncodeProgramIR(ProgramIR{
			Name: "geometric_probe",
			Slots: []SlotIR{
				{Op: MachineOp{Opcode: OpGeometricCompose}},
			},
		}, Layout{})

		Convey("It should preserve the raw opcode word", func() {
			So(err, ShouldBeNil)
			So(ir.Words[0], ShouldEqual, uint64(OpGeometricCompose))
			So(len(ir.Words), ShouldEqual, 16)

			_, _, _, _, _, _, opcode, _, _, _, _, _, _, _, _, _, _, _ := DecodeInstruction(ir.Words[0])
			So(opcode, ShouldEqual, uint64(OpGeometricCompose))
		})
	})

	Convey("Given a geometric slot with boolean fields attached", t, func() {
		_, err := EncodeProgramIR(ProgramIR{
			Name: "bad_geometric",
			Slots: []SlotIR{
				{Op: MachineOp{Opcode: OpGeometricCompose, ASpan: 1}},
			},
		}, Layout{})

		Convey("It should reject the mixed slot shape", func() {
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "geometric opcode")
			So(err.Error(), ShouldContainSubstring, "raw slot")
		})
	})

	Convey("Given an IR program matching a feed truth-table assignment", t, func() {
		source := `
program copy {
  set A.signals[0,2] <- xor(A.tokens[0,2], A.context[0,2])
}`

		feed, feedErr := Compile(nil, source, Layout{})
		ir, irErr := EncodeProgramIR(ProgramIR{
			Name: "copy",
			Slots: []SlotIR{
				{
					Op: MachineOp{
						Opcode:    OpXor,
						AStart:    0,
						ASpan:     2,
						BStart:    40,
						BSpan:     2,
						DstStart:  32,
						DstSpan:   2,
						MaskStart: DefaultMaskTrueWord,
					},
				},
			},
		}, Layout{})

		Convey("It should encode the same first machine word without using feed text", func() {
			So(feedErr, ShouldBeNil)
			So(irErr, ShouldBeNil)
			So(ir.Words[0], ShouldEqual, feed.Words[0])
			So(len(ir.Words), ShouldEqual, 16)
			So(ir.MaskTrueWord, ShouldEqual, DefaultMaskTrueWord)
		})
	})

	Convey("Given more than sixteen slots", t, func() {
		slots := make([]SlotIR, 17)

		_, err := EncodeProgramIR(ProgramIR{Name: "oversized", Slots: slots}, Layout{})

		Convey("It should reject the program before packing truncated words", func() {
			So(err, ShouldNotBeNil)
		})
	})

	Convey("Given a non-empty slot that packs to a zero word", t, func() {
		_, err := EncodeProgramIR(ProgramIR{
			Name:  "empty",
			Slots: []SlotIR{{Op: MachineOp{}}},
		}, Layout{})

		Convey("It should reject the slot instead of encoding an accidental halt", func() {
			So(err, ShouldNotBeNil)
		})
	})

	Convey("Given an IR program with B-side rot8 alignment", t, func() {
		source := `
program align {
  set A.signals[0,2] <- xor(A.tokens[0,2], rot8(B.tokens[0,2], 3))
}`

		feed, feedErr := Compile(nil, source, Layout{})
		ir, irErr := EncodeProgramIR(ProgramIR{
			Name: "align",
			Slots: []SlotIR{
				{
					Op: MachineOp{
						Opcode:        OpXor,
						AStart:        0,
						ASpan:         2,
						BStart:        0,
						BSpan:         2,
						DstStart:      32,
						DstSpan:       2,
						MaskStart:     72,
						PredicateCond: 3,
					},
				},
			},
		}, Layout{})

		Convey("It should encode rotation in the predicate-condition field", func() {
			So(feedErr, ShouldBeNil)
			So(irErr, ShouldBeNil)
			So(ir.Words[0], ShouldEqual, feed.Words[0])

			_, _, _, _, _, _, _, _, _, _, predCond,
				legacyIndirect, legacyBType, predicate,
				_, _, _, _ := DecodeInstruction(ir.Words[0])
			So(legacyIndirect, ShouldEqual, uint64(0))
			So(legacyBType, ShouldEqual, uint64(0))
			So(predicate, ShouldEqual, uint64(0))
			So(predCond, ShouldEqual, uint64(3))
		})
	})

	Convey("Given an IR program with a scalar rotate", t, func() {
		source := `
program rotate {
  set A.signals[0,1] <- rotl(A.tokens[0,1], 8)
}`

		feed, feedErr := Compile(nil, source, Layout{})
		ir, irErr := EncodeProgramIR(ProgramIR{
			Name: "rotate",
			Slots: []SlotIR{
				{
					Op: MachineOp{
						Opcode:        ScalarRotateLeft,
						AStart:        0,
						ASpan:         1,
						BStart:        73,
						BSpan:         1,
						DstStart:      32,
						DstSpan:       1,
						MaskStart:     72,
						Predicate:     true,
						PredicateCond: PredScalar,
					},
				},
			},
			Constants: []ConstantInit{{Offset: 73, Value: 8}},
		}, Layout{})

		Convey("It should encode the same scalar word without feed text", func() {
			So(feedErr, ShouldBeNil)
			So(irErr, ShouldBeNil)
			So(ir.Words[0], ShouldEqual, feed.Words[0])
		})
	})

	Convey("Given an IR program with legacy stage/pop bits", t, func() {
		_, err := EncodeProgramIR(ProgramIR{
			Name: "legacy",
			Slots: []SlotIR{
				{Op: MachineOp{Opcode: OpCopyA, ASpan: 1, BSpan: 1, DstSpan: 1, MaskStart: 72, Stage: true}},
			},
		}, Layout{})

		Convey("It should reject non-linear control bits", func() {
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "strict linear ALU")
		})
	})
}

func TestFormatProgramSweep16(t *testing.T) {
	Convey("Given a one-instruction IR program", t, func() {
		compiled, err := EncodeProgramIR(ProgramIR{
			Name: "copy",
			Slots: []SlotIR{
				{
					Op: MachineOp{
						Opcode:    OpCopyA,
						AStart:    122,
						ASpan:     1,
						BStart:    122,
						BSpan:     1,
						DstStart:  122,
						DstSpan:   1,
						MaskStart: DefaultMaskTrueWord,
					},
				},
			},
		}, Layout{})

		Convey("It should print exactly sixteen explicit sweep slots", func() {
			So(err, ShouldBeNil)

			sweep := FormatProgramSweep16(compiled.Words)
			lines := strings.Split(sweep, "\n")

			So(len(lines), ShouldEqual, 16)
			So(lines[0], ShouldContainSubstring, "slot 00:")
			So(lines[1], ShouldEqual, "slot 01: empty")
			So(lines[15], ShouldEqual, "slot 15: empty")
		})
	})
}

func BenchmarkFormatProgramSweep16(b *testing.B) {
	compiled, err := EncodeProgramIR(ProgramIR{
		Name: "copy",
		Slots: []SlotIR{
			{
				Op: MachineOp{
					Opcode:    OpCopyA,
					AStart:    122,
					ASpan:     1,
					BStart:    122,
					BSpan:     1,
					DstStart:  122,
					DstSpan:   1,
					MaskStart: DefaultMaskTrueWord,
				},
			},
		},
	}, Layout{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		FormatProgramSweep16(compiled.Words)
	}
}

func BenchmarkEncodeProgramIR(b *testing.B) {
	ir := ProgramIR{
		Name: "copy",
		Slots: []SlotIR{
			{
				Op: MachineOp{
					Opcode:    OpXor,
					AStart:    0,
					ASpan:     2,
					BStart:    40,
					BSpan:     2,
					DstStart:  32,
					DstSpan:   2,
					MaskStart: 72,
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := EncodeProgramIR(ir, Layout{}); err != nil {
			b.Fatal(err)
		}
	}
}
