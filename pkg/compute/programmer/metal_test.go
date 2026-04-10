package programmer

import (
	"math/bits"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCompilerMetalTransposedRotationBanks(t *testing.T) {
	t.Parallel()

	Convey("Compile(Metal) stores B rotations in SIMD-friendly transposed banks", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("transpose")))

		So(err, ShouldBeNil)

		defer raw.Close()

		source := []uint64{0xaaaafff00000eeee, 0x1111222233334444, 0x0102030405060708, 0xfedcba9876543210}

		intent := Intent{Operation: Similarity, Assets: [][]uint64{source}}

		New(raw, CompilerWithIntent(intent)).Compile(Metal)

		progWord := core.Cfg.Value.Region.Program.Start

		So(wordFromBytes(raw, progWord)&0xF, ShouldEqual, uint64(Similarity)&0xF)

		for rot := range 16 {
			w0 := source[0]
			w1 := source[1]
			w2 := source[2]
			w3 := source[3]

			for range rot {
				w0 = bits.RotateLeft64(w0, 8)
				w1 = bits.RotateLeft64(w1, 8)
				w2 = bits.RotateLeft64(w2, 8)
				w3 = bits.RotateLeft64(w3, 8)
			}

			So(wordFromBytes(raw, 32+rot), ShouldEqual, w0)
			So(wordFromBytes(raw, 48+rot), ShouldEqual, w1)
			So(wordFromBytes(raw, 64+rot), ShouldEqual, w2)
			So(wordFromBytes(raw, 80+rot), ShouldEqual, w3)
		}
	})
}

func BenchmarkCompilerMetalExpandTransposedPath(b *testing.B) {
	raw, err := primitive.FirstSegment(primitive.NewValue([]byte("metal-rot")))

	if err != nil {
		b.Fatal(err)
	}

	defer raw.Close()

	source := []uint64{0x1, 0x2, 0x3, 0x4}

	intent := Intent{Operation: Similarity, Assets: [][]uint64{source}}

	compiler := New(raw, CompilerWithIntent(intent))

	b.ResetTimer()

	for b.Loop() {
		compiler.Compile(Metal)
	}
}
