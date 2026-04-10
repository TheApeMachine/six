package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCompilerCompileMetalGPUProgramLayout(t *testing.T) {
	t.Parallel()

	Convey("Compile(Metal) writes opcode to the configured program region", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("metal-op")))

		So(err, ShouldBeNil)

		defer raw.Close()

		progWord := core.Cfg.Value.Region.Program.Start

		intent := Intent{Operation: Bundle, Assets: [][]uint64{{1, 2, 3, 4}}}

		New(raw, CompilerWithIntent(intent)).Compile(Metal)

		So(wordFromBytes(raw, progWord)&0xF, ShouldEqual, uint64(Bundle)&0xF)
	})

	Convey("Compile(Metal) leaves pass count at word 124 when one asset fits the rotation arena", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("passes")))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{
			Operation: Similarity,
			Assets:    [][]uint64{{1, 2, 3, 4}},
		}

		New(raw, CompilerWithIntent(intent)).Compile(Metal)

		So(wordFromBytes(raw, 124), ShouldEqual, 1)
	})

	Convey("Compile(Metal) uses pass count 1 when no assets are listed", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("empty-pass")))

		So(err, ShouldBeNil)

		defer raw.Close()

		intent := Intent{Operation: Distance, Assets: nil}

		New(raw, CompilerWithIntent(intent)).Compile(Metal)

		So(wordFromBytes(raw, 124), ShouldEqual, 1)
	})

	Convey("Compile(Metal) with batch affinity skips rotation banks like CPU", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("m-affinity")))

		So(err, ShouldBeNil)

		defer raw.Close()

		asset := make([]uint64, primitive.AffinityWords)

		asset[0] = 0xc0de

		intent := Intent{Operation: Distance, Assets: [][]uint64{asset}}

		New(
			raw,
			CompilerWithIntent(intent),
			CompilerWithBatchAffinityLayout(),
		).Compile(Metal)

		So(wordFromBytes(raw, kernel.NearestAffinityCandidatesStartWord), ShouldEqual, 0xc0de)
	})
}

func BenchmarkCompilerCompileMetalGPUProgramLayout(b *testing.B) {
	raw, err := primitive.FirstSegment(primitive.NewValue([]byte("mbench")))

	if err != nil {
		b.Fatal(err)
	}

	defer raw.Close()

	intent := Intent{
		Operation: Similarity,
		Assets:    [][]uint64{{1, 2, 3, 4}, {5, 6, 7, 8}},
	}

	compiler := New(raw, CompilerWithIntent(intent))

	b.ResetTimer()

	for b.Loop() {
		compiler.Compile(Metal)
	}
}
