package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewBuilder checks that the builder holds the token slice reference for lowering.
*/
func TestNewBuilder(t *testing.T) {
	Convey("Given a non-empty token slice", t, func() {
		tokens := []Token{
			{SrcA: FullRegionRef(primitive.TokenRegion), SrcB: FullRegionRef(primitive.TokenRegion), Dst: FullRegionRef(primitive.SignalsRegion), Op: XOR, Mode: ModeAccumulate},
		}

		builder := NewBuilder(tokens)

		Convey("NewBuilder should retain those tokens for build", func() {
			So(builder, ShouldNotBeNil)
			So(len(builder.tokens), ShouldEqual, len(tokens))
			So(builder.tokens[0].Op, ShouldEqual, XOR)
		})
	})
}

/*
TestBuilder_build validates one frame per token and stable lowering into Program words.
*/
func TestBuilder_build(t *testing.T) {
	Convey("Given tokens for xor over token lanes into signals", t, func() {
		tokens := []Token{
			{
				SrcA: FullRegionRef(primitive.TokenRegion),
				SrcB: FullRegionRef(primitive.TokenRegion),
				Dst:  FullRegionRef(primitive.SignalsRegion),
				Op:   XOR,
				Mode: ModeAccumulate,
			},
		}

		builder := NewBuilder(tokens)

		Convey("build should emit one frame with opcode, contract, and region packing", func() {
			frames, err := builder.build(CPU)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)

			frame := frames[0]
			So(frame.Contract, ShouldEqual, ContractSweepSignal)
			So(frame.Program[0], ShouldEqual, uint64(XOR)&0xF)

			aStart, aSpan := kernel.UnpackRegionRef(frame.Program[3])
			wantAStart, wantAWords := primitive.TokenRegion.WordExtent()

			So(aStart, ShouldEqual, wantAStart)
			So(aSpan, ShouldEqual, wantAWords)

			dstStart, dstSpan := kernel.UnpackRegionRef(frame.Program[5])
			wantDstStart, wantDstWords := primitive.SignalsRegion.WordExtent()

			So(dstStart, ShouldEqual, wantDstStart)
			So(dstSpan, ShouldEqual, wantDstWords)
		})
	})
}

/*
TestBuilder_packTruth checks the legacy opcode nibble, rotation table, and mode word.
*/
func TestBuilder_packTruth(t *testing.T) {
	Convey("Given a builder and xor token", t, func() {
		builder := NewBuilder(nil)
		tok := Token{
			SrcA: FullRegionRef(primitive.TokenRegion),
			SrcB: FullRegionRef(primitive.ContextRegion),
			Dst:  FullRegionRef(primitive.GradientRegion),
			Op:   XOR,
			Mode: ModeReduce,
		}

		var frame Frame

		Convey("packTruth should fill opcode, replicated rotation table, and mode", func() {
			frame.Contract = ContractReduceBinary
			builder.packTruth(&frame, tok.Op, tok)

			nibble := uint64(XOR) & 0xF

			So(frame.Program[0], ShouldEqual, nibble)

			var wantTable uint64

			for rotation := 0; rotation < 16; rotation++ {
				wantTable |= nibble << (rotation * 4)
			}

			So(frame.Program[1], ShouldEqual, wantTable)
			So(frame.Program[2]&0xFF, ShouldEqual, uint64(ModeReduce))
			So((frame.Program[2]>>kernel.ProgramContractShift)&0xFF, ShouldEqual, kernel.ProgramContractReduce)
		})

		Convey("packTruth should preserve full geometric opcode bytes", func() {
			geometric := Token{
				SrcA: FullRegionRef(primitive.ContextRegion),
				SrcB: FullRegionRef(primitive.GradientRegion),
				Dst:  FullRegionRef(primitive.SignalsRegion),
				Op:   COMPOSE,
				Mode: ModeAccumulate,
			}

			frame = Frame{Contract: ContractGeometric}
			builder.packTruth(&frame, geometric.Op, geometric)

			So(frame.Program[0], ShouldEqual, uint64(kernel.OpcodeGeometricCompose))
			So(frame.Program[1], ShouldEqual, uint64(0))
			So((frame.Program[2]>>kernel.ProgramContractShift)&0xFF, ShouldEqual, kernel.ProgramContractGeometric)
		})
	})
}

/*
TestBuilder_contractFor classifies token lowering into execution contract
families so later compiler work can stop treating every binary line as the same
sweep path.
*/
func TestBuilder_contractFor(t *testing.T) {
	Convey("Given a builder receiver", t, func() {
		builder := NewBuilder(nil)

		Convey("contractFor should classify link-style writes to prev/next as exact binary", func() {
			contract := builder.contractFor(Token{
				SrcA: RegionRef{Region: primitive.AssetRegion, Start: 56, Span: 1},
				SrcB: RegionRef{Region: primitive.AssetRegion, Start: 56, Span: 1},
				Dst:  RegionRef{Region: primitive.PrevRegion, Start: 120, Span: 1},
				Op:   OR,
				Mode: ModeAccumulate,
			})

			So(contract, ShouldEqual, ContractExactBinary)
		})

		Convey("build should encode exact-binary contract metadata into the frame", func() {
			tokens := []Token{{
				SrcA: RegionRef{Region: primitive.AssetRegion, Start: 56, Span: 1},
				SrcB: RegionRef{Region: primitive.AssetRegion, Start: 57, Span: 1},
				Dst:  RegionRef{Region: primitive.PrevRegion, Start: 120, Span: 1},
				Op:   OR,
				Mode: ModeAccumulate,
			}}
			frames, err := NewBuilder(tokens).build(CPU)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Contract, ShouldEqual, ContractExactBinary)
			So((frames[0].Program[2]>>kernel.ProgramContractShift)&0xFF, ShouldEqual, kernel.ProgramContractExactBinary)
			So(frames[0].Program[2]&0xFF, ShouldEqual, uint64(ModeAccumulate))
		})

		Convey("contractFor should classify reduce lines as reduce binary", func() {
			contract := builder.contractFor(Token{
				SrcA: FullRegionRef(primitive.SignalsRegion),
				SrcB: FullRegionRef(primitive.SignalsRegion),
				Dst:  RegionRef{Region: primitive.PropertiesRegion, Start: 48, Span: 1},
				Op:   XOR,
				Mode: ModeReduce,
			})

			So(contract, ShouldEqual, ContractReduceBinary)
		})

		Convey("contractFor should classify signal-style accumulations as sweep signal", func() {
			contract := builder.contractFor(Token{
				SrcA: FullRegionRef(primitive.TokenRegion),
				SrcB: FullRegionRef(primitive.ContextRegion),
				Dst:  FullRegionRef(primitive.SignalsRegion),
				Op:   XOR,
				Mode: ModeAccumulate,
			})

			So(contract, ShouldEqual, ContractSweepSignal)
		})

		Convey("contractFor should classify compose as geometric", func() {
			contract := builder.contractFor(Token{
				SrcA: FullRegionRef(primitive.ContextRegion),
				SrcB: FullRegionRef(primitive.GradientRegion),
				Dst:  FullRegionRef(primitive.SignalsRegion),
				Op:   COMPOSE,
				Mode: ModeAccumulate,
			})

			So(contract, ShouldEqual, ContractGeometric)
		})
	})
}

func BenchmarkBuilder_build(b *testing.B) {
	tokens := []Token{
		{
			SrcA: FullRegionRef(primitive.TokenRegion),
			SrcB: FullRegionRef(primitive.TokenRegion),
			Dst:  FullRegionRef(primitive.SignalsRegion),
			Op:   XOR,
			Mode: ModeAccumulate,
		},
	}

	builder := NewBuilder(tokens)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = builder.build(CPU)
	}
}

func BenchmarkBuilder_packTruth(b *testing.B) {
	builder := NewBuilder(nil)
	tok := Token{
		SrcA: FullRegionRef(primitive.TokenRegion),
		SrcB: FullRegionRef(primitive.TokenRegion),
		Dst:  FullRegionRef(primitive.SignalsRegion),
		Op:   XOR,
		Mode: ModeAccumulate,
	}

	var frame Frame

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		builder.packTruth(&frame, tok.Op, tok)
	}
}
