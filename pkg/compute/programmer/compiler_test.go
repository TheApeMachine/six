package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewCompiler verifies the compiler wires the builder over the token slice.
*/
func TestNewCompiler(t *testing.T) {
	Convey("Given parsed tokens", t, func() {
		tokens := []Token{
			{SrcA: primitive.TokenRegion, SrcB: primitive.TokenRegion, Dst: primitive.SignalsRegion, Op: AND, Mode: ModeAccumulate},
		}

		compiler := NewCompiler(tokens)

		Convey("NewCompiler should produce a compiler whose Compile uses those tokens", func() {
			So(compiler, ShouldNotBeNil)
			So(compiler.builder, ShouldNotBeNil)
			So(len(compiler.builder.tokens), ShouldEqual, 1)
		})
	})
}

/*
TestCompiler_Compile delegates to Builder.build for each backend target.
*/
func TestCompiler_Compile(t *testing.T) {
	Convey("Given a compiler built from xor tokens", t, func() {
		tokens := []Token{
			{
				SrcA: primitive.TokenRegion,
				SrcB: primitive.TokenRegion,
				Dst:  primitive.SignalsRegion,
				Op:   XOR,
				Mode: ModeAccumulate,
			},
		}

		compiler := NewCompiler(tokens)

		Convey("Compile(CPU) should return the same lowering as Builder.build", func() {
			frames, err := compiler.Compile(CPU)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Program[0], ShouldEqual, uint64(XOR)&0xF)
		})

		Convey("Compile(Metal) should still emit frames (target reserved)", func() {
			frames, err := compiler.Compile(Metal)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
		})
	})
}

func BenchmarkNewCompiler(b *testing.B) {
	tokens := []Token{
		{SrcA: primitive.TokenRegion, SrcB: primitive.TokenRegion, Dst: primitive.SignalsRegion, Op: XOR, Mode: ModeAccumulate},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = NewCompiler(tokens)
	}
}

func BenchmarkCompiler_Compile(b *testing.B) {
	tokens := []Token{
		{
			SrcA: primitive.TokenRegion,
			SrcB: primitive.TokenRegion,
			Dst:  primitive.SignalsRegion,
			Op:   XOR,
			Mode: ModeAccumulate,
		},
	}

	compiler := NewCompiler(tokens)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = compiler.Compile(CPU)
	}
}
