package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMetalCompiler_Compile(t *testing.T) {
	Convey("Given MetalCompiler with or token", t, func() {
		compiler := NewMetalCompiler([]Token{{Op: "or"}})

		Convey("Compile should produce one frame with or nibble", func() {
			frames, err := compiler.Compile()

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Program[0], ShouldEqual, uint64(OR&0xF))
		})
	})
}

func BenchmarkMetalCompiler_Compile(b *testing.B) {
	compiler := NewMetalCompiler([]Token{{Op: "xor"}})

	b.ResetTimer()

	for range b.N {
		_, _ = compiler.Compile()
	}
}
