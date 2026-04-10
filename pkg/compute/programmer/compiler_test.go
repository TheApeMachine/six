package programmer

import (
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func wordFromBytes(value *primitive.Value, wordIdx int) uint64 {
	slab := value.Bytes()

	return binary.LittleEndian.Uint64(slab[wordIdx*8 : wordIdx*8+8])
}

func TestNewCompiler(t *testing.T) {
	t.Parallel()

	Convey("New attaches options and Frame returns the Value", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("probe")))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{Operation: Similarity, Assets: [][]uint64{{1, 2, 3, 4}}}
		compiler := New(
			raw,
			CompilerWithIntent(intent),
			CompilerWithBatchAffinityLayout(),
			CompilerWithFinalizer(func(value *primitive.Value, next FinalizeNext) ([]*primitive.Value, error) {
				return next(value)
			}),
		)

		So(compiler.Frame(), ShouldEqual, raw)
		So(compiler.Intent().Operation, ShouldEqual, Similarity)
		So(compiler.FinalizerDepth(), ShouldEqual, 1)
		So(compiler.UsesBatchAffinityLayout(), ShouldBeTrue)
	})
}

func TestCompilerCompile(t *testing.T) {
	t.Parallel()

	Convey("Compile(CPU) writes opcode and rotation passes", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("layout")))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{
			Operation: Similarity,
			Assets:    [][]uint64{{0xa, 0xb, 0xc, 0xd}},
		}

		compiler := New(raw, CompilerWithIntent(intent))
		out := compiler.Compile(CPU)

		So(out, ShouldEqual, raw)

		So(
			wordFromBytes(raw, core.Cfg.Value.Region.Program.Start)&0xF,
			ShouldEqual,
			uint64(Similarity)&0xF,
		)
		So(wordFromBytes(raw, 124), ShouldBeGreaterThan, uint64(0))
	})
}

func TestCompilerCompileBatchAffinity(t *testing.T) {
	t.Parallel()

	Convey("Compile(CPU) with batch layout writes candidates at NearestAffinityCandidatesStartWord", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("affinity")))

		So(err, ShouldBeNil)

		defer raw.Close()

		asset := make([]uint64, primitive.AffinityWords)

		asset[0] = 0xfeed

		intent := Intent{
			Operation: Distance,
			Assets:    [][]uint64{asset},
		}

		compiler := New(
			raw,
			CompilerWithIntent(intent),
			CompilerWithBatchAffinityLayout(),
		)

		compiler.Compile(CPU)

		So(wordFromBytes(raw, kernel.NearestAffinityCandidatesStartWord), ShouldEqual, 0xfeed)
	})
}

func TestCompilerCompileGeometric(t *testing.T) {
	t.Parallel()

	Convey("Compile(CPU) writes geometric operands without lowering the opcode nibble", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("pga-layout")))

		So(err, ShouldBeNil)

		defer raw.Close()

		left := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
		right := []uint64{9, 10, 11, 12, 13, 14, 15, 16}
		intent := Intent{
			Operation: GeometricSandwich,
			Assets:    [][]uint64{left, right},
		}

		New(raw, CompilerWithIntent(intent)).Compile(CPU)

		So(
			wordFromBytes(raw, core.Cfg.Value.Region.Program.Start)&0xFF,
			ShouldEqual,
			uint64(kernel.OpcodeGeometricSandwich),
		)
		So(wordFromBytes(raw, kernel.ContextStartWord), ShouldEqual, left[0])
		So(wordFromBytes(raw, kernel.ContextStartWord+7), ShouldEqual, left[7])
		So(wordFromBytes(raw, kernel.GradientStartWord), ShouldEqual, right[0])
		So(wordFromBytes(raw, kernel.GradientStartWord+7), ShouldEqual, right[7])
	})
}

func BenchmarkCompilerCompileCPU(b *testing.B) {
	raw, err := primitive.FirstSegment(primitive.NewValue([]byte("bench")))

	if err != nil {
		b.Fatal(err)
	}

	defer raw.Close()

	intent := Intent{
		Operation: Similarity,
		Assets:    [][]uint64{{1, 2, 3, 4}},
	}

	compiler := New(raw, CompilerWithIntent(intent))

	b.ResetTimer()

	for b.Loop() {
		compiler.Compile(CPU)
	}
}
