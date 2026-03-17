package data

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestCompileSequenceCells(t *testing.T) {
	coder := NewMortonCoder()
	keys := []uint64{
		coder.Pack(0, 'a'),
		coder.Pack(1, 'b'),
		coder.Pack(0, 'a'),
	}

	calc := numeric.NewCalculus()
	aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
	abPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
	abaPhase := calc.Multiply(abPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))

	Convey("Given a reset-aware tokenizer key stream", t, func() {
		cells := CompileSequenceCells(keys)

		Convey("It should compile one native program cell per key", func() {
			So(len(cells), ShouldEqual, 3)
			So(cells[0].Position, ShouldEqual, uint32(0))
			So(cells[1].Position, ShouldEqual, uint32(1))
			So(cells[2].Position, ShouldEqual, uint32(0))
		})

		Convey("It should keep lexical identity out of the native values", func() {
			So(HasLexicalSeed(cells[0].Value, 'a'), ShouldBeFalse)
			So(HasLexicalSeed(cells[1].Value, 'b'), ShouldBeFalse)
			So(HasLexicalSeed(cells[2].Value, 'a'), ShouldBeFalse)
		})

		Convey("It should encode the cumulative GF(257) state at each cell", func() {
			So(cells[0].Value.ResidualCarry(), ShouldEqual, uint64(aPhase))
			So(cells[1].Value.ResidualCarry(), ShouldEqual, uint64(abPhase))
			So(cells[2].Value.ResidualCarry(), ShouldEqual, uint64(abaPhase))

			So(cells[0].Value.Has(int(aPhase)), ShouldBeTrue)
			So(cells[1].Value.Has(int(abPhase)), ShouldBeTrue)
			So(cells[2].Value.Has(int(abaPhase)), ShouldBeTrue)
		})

		Convey("It should compile program flow from local-depth transitions", func() {
			opcode0, jump0, branches0, terminal0 := cells[0].Value.Program()
			So(opcode0, ShouldEqual, OpcodeNext)
			So(jump0, ShouldEqual, uint32(1))
			So(branches0, ShouldEqual, uint8(0))
			So(terminal0, ShouldBeFalse)

			opcode1, jump1, branches1, terminal1 := cells[1].Value.Program()
			So(opcode1, ShouldEqual, OpcodeReset)
			So(jump1, ShouldEqual, uint32(0))
			So(branches1, ShouldEqual, uint8(1))
			So(terminal1, ShouldBeFalse)

			opcode2, jump2, branches2, terminal2 := cells[2].Value.Program()
			So(opcode2, ShouldEqual, OpcodeHalt)
			So(jump2, ShouldEqual, uint32(0))
			So(branches2, ShouldEqual, uint8(0))
			So(terminal2, ShouldBeTrue)
		})

		Convey("It should encode next-symbol transitions as affine trajectory", func() {
			scale0, translate0 := cells[0].Value.Affine()
			So(scale0, ShouldEqual, PhaseScaleForByte('b'))
			So(translate0, ShouldEqual, numeric.Phase(0))

			from0, to0, ok0 := cells[0].Value.Trajectory()
			So(ok0, ShouldBeTrue)
			So(from0, ShouldEqual, aPhase)
			So(to0, ShouldEqual, abPhase)

			scale1, translate1 := cells[1].Value.Affine()
			So(scale1, ShouldEqual, PhaseScaleForByte('a'))
			So(translate1, ShouldEqual, numeric.Phase(0))

			from1, to1, ok1 := cells[1].Value.Trajectory()
			So(ok1, ShouldBeTrue)
			So(from1, ShouldEqual, abPhase)
			So(to1, ShouldEqual, abaPhase)
		})
	})
}

func TestCompileObservableSequenceValues(t *testing.T) {
	coder := NewMortonCoder()
	keys := []uint64{
		coder.Pack(0, 'x'),
		coder.Pack(1, 'y'),
	}

	Convey("Given compiled observable prompt values", t, func() {
		values := CompileObservableSequenceValues(keys)

		Convey("They should preserve lexical seeds for projection", func() {
			So(len(values), ShouldEqual, 2)
			So(HasLexicalSeed(values[0], 'x'), ShouldBeTrue)
			So(HasLexicalSeed(values[1], 'y'), ShouldBeTrue)
		})

		Convey("They should still carry native phase/program state underneath the projection", func() {
			So(values[0].ResidualCarry(), ShouldBeGreaterThan, uint64(0))
			So(values[1].ResidualCarry(), ShouldBeGreaterThan, uint64(0))

			opcode0, _, _, _ := values[0].Program()
			opcode1, _, _, terminal1 := values[1].Program()
			So(opcode0, ShouldEqual, OpcodeNext)
			So(opcode1, ShouldEqual, OpcodeHalt)
			So(terminal1, ShouldBeTrue)
		})
	})
}

func BenchmarkCompileSequenceCells(b *testing.B) {
	coder := NewMortonCoder()
	keys := []uint64{
		coder.Pack(0, 'a'),
		coder.Pack(1, 'b'),
		coder.Pack(0, 'a'),
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		CompileSequenceCells(keys)
	}
}

func BenchmarkCompileObservableSequenceValues(b *testing.B) {
	coder := NewMortonCoder()
	keys := []uint64{
		coder.Pack(0, 'a'),
		coder.Pack(1, 'b'),
		coder.Pack(0, 'a'),
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		CompileObservableSequenceValues(keys)
	}
}
