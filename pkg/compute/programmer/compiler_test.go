package programmer

import (
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func wordFromBytes(value *primitive.Value, wordIdx int) uint64 {
	slab := value.Bytes()

	return binary.LittleEndian.Uint64(slab[wordIdx*8 : wordIdx*8+8])
}

func TestNewCompiler(t *testing.T) {
	t.Parallel()

	Convey("New attaches options and Frame returns the Value", t, func() {
		raw, err := primitive.NewValue([]byte("probe"))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{Operation: Similarity, Assets: [][]uint64{{1, 2, 3, 4}}}
		compiler := New(
			raw,
			CompilerWithIntent(intent),
			CompilerWithBatchAffinityLayout(),
		)

		So(compiler.Frame(), ShouldEqual, raw)
	})
}

func TestCompilerCompile(t *testing.T) {
	t.Parallel()

	Convey("Compile(CPU) writes opcode and rotation passes", t, func() {
		raw, err := primitive.NewValue([]byte("layout"))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{
			Operation: Similarity,
			Assets:    [][]uint64{{0xa, 0xb, 0xc, 0xd}},
		}

		compiler := New(raw, CompilerWithIntent(intent))
		out := compiler.Compile(CPU)

		So(out, ShouldEqual, raw)

		So(wordFromBytes(raw, 16)&0xF, ShouldEqual, uint64(Similarity)&0xF)
		So(wordFromBytes(raw, 124), ShouldBeGreaterThan, uint64(0))
	})
}

func TestCompilerCompileBatchAffinity(t *testing.T) {
	t.Parallel()

	Convey("Compile(CPU) with batch layout writes candidates from word 48", t, func() {
		raw, err := primitive.NewValue([]byte("affinity"))

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

		So(wordFromBytes(raw, 48), ShouldEqual, 0xfeed)
	})
}

func BenchmarkCompilerCompileCPU(b *testing.B) {
	raw, err := primitive.NewValue([]byte("bench"))

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
