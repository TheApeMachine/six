package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestNewCompiler(t *testing.T) {
	Convey("Given tokens and a continuation option", t, func() {
		toks := []Token{{Op: "xor", SrcA: "tokens[0]", SrcB: "tokens[1]", Dst: "signals[0]", Mode: "accumulate"}}
		cont := &Continuation{Kind: ContinuationValueID, ValueID: 7}

		compiler := NewCompiler(toks, WithContinuation(cont))

		Convey("NewCompiler should retain tokens and continuation", func() {
			So(compiler.Tokens(), ShouldResemble, toks)
			So(compiler.Continuation(), ShouldResemble, cont)
		})
	})
}

func TestCompiler_Tokens(t *testing.T) {
	Convey("Given a compiler constructed with a token slice", t, func() {
		toks := []Token{{Op: "and"}}
		compiler := NewCompiler(toks)

		Convey("Tokens should return the same slice reference", func() {
			So(compiler.Tokens(), ShouldEqual, toks)
		})
	})
}

func TestCompiler_Continuation(t *testing.T) {
	Convey("Given NewCompiler without WithContinuation", t, func() {
		compiler := NewCompiler([]Token{{Op: "or"}})

		Convey("Continuation should be nil", func() {
			So(compiler.Continuation(), ShouldBeNil)
		})
	})
}

func TestCompiler_Compile(t *testing.T) {
	original := *core.Cfg
	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given truth-table tokens", t, func() {
		toks := []Token{
			{Op: "xor", SrcA: "tokens[0]", SrcB: "tokens[1]", Dst: "signals[0]", Mode: "accumulate"},
			{Op: "and", SrcA: "tokens[0]", SrcB: "tokens[1]", Dst: "signals[0]", Mode: "reduce"},
		}
		compiler := NewCompiler(toks)

		Convey("Compile with CPU should emit one frame per token with packed program words", func() {
			frames, err := compiler.Compile(CPU)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 2)
			So(frames[0].Program[0], ShouldEqual, uint64(XOR&0xF))
			So(frames[1].Program[0], ShouldEqual, uint64(AND&0xF))
		})

		Convey("Compile with Metal should match CPU frame payload for the same ops", func() {
			cpuFrames, errCPU := compiler.Compile(CPU)
			metalFrames, errMetal := compiler.Compile(Metal)

			So(errCPU, ShouldBeNil)
			So(errMetal, ShouldBeNil)
			So(metalFrames[0].Program[0], ShouldEqual, cpuFrames[0].Program[0])
			So(metalFrames[0].Program[1], ShouldEqual, cpuFrames[0].Program[1])
		})

		Convey("Compile with CUDA should match CPU frame payload for the same ops", func() {
			cpuFrames, errCPU := compiler.Compile(CPU)
			cudaFrames, errCUDA := compiler.Compile(CUDA)

			So(errCPU, ShouldBeNil)
			So(errCUDA, ShouldBeNil)
			So(cudaFrames[0].Program[0], ShouldEqual, cpuFrames[0].Program[0])
		})
	})

	Convey("Given a token with popcount (surface-only op)", t, func() {
		compiler := NewCompiler([]Token{{Op: "popcount"}})

		Convey("Compile should fail until lowering exists", func() {
			_, err := compiler.Compile(CPU)

			So(err, ShouldNotBeNil)
		})
	})

	Convey("Given an invalid compiler target", t, func() {
		compiler := NewCompiler([]Token{{Op: "xor"}})

		Convey("Compile should return unsupported target", func() {
			_, err := compiler.Compile(CompilerTarget(255))

			So(err, ShouldNotBeNil)
		})
	})
}

func TestFrameBuilder_frames(t *testing.T) {
	Convey("Given a frameBuilder with xor and nor tokens", t, func() {
		builder := newFrameBuilder([]Token{
			{Op: "xor"},
			{Op: "nor"},
		})

		Convey("frames should pack sixteen nibbles in Program[1]", func() {
			out, err := builder.frames(CPU)

			So(err, ShouldBeNil)
			So(len(out), ShouldEqual, 2)

			n := uint64(XOR & 0xF)
			var want uint64

			for rotation := 0; rotation < 16; rotation++ {
				want |= n << (rotation * 4)
			}

			So(out[0].Program[1], ShouldEqual, want)
		})
	})
}

func TestFrameBuilder_acceptSourceOp(t *testing.T) {
	Convey("Given a frameBuilder", t, func() {
		builder := newFrameBuilder(nil)

		Convey("acceptSourceOp should allow popcount without lowering", func() {
			So(builder.acceptSourceOp("popcount"), ShouldBeNil)
		})

		Convey("acceptSourceOp should reject unknown mnemonics", func() {
			So(builder.acceptSourceOp("not_an_op"), ShouldNotBeNil)
		})
	})
}

func BenchmarkCompiler_Compile(b *testing.B) {
	original := *core.Cfg
	b.Cleanup(func() {
		*core.Cfg = original
	})

	toks := []Token{
		{Op: "xor", SrcA: "tokens[0]", SrcB: "tokens[1]", Dst: "signals[0]", Mode: "accumulate"},
	}
	compiler := NewCompiler(toks)

	b.ResetTimer()

	for range b.N {
		_, _ = compiler.Compile(CPU)
	}
}
