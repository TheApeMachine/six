package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCUDACompiler_Compile(t *testing.T) {
	Convey("Given CUDACompiler with xnor token", t, func() {
		compiler := NewCUDACompiler([]Token{{Op: "xnor"}})

		Convey("Compile should produce one frame with xnor nibble", func() {
			frames, err := compiler.Compile()

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Program[0], ShouldEqual, uint64(XNOR&0xF))
		})
	})
}

func BenchmarkCUDACompiler_Compile(b *testing.B) {
	compiler := NewCUDACompiler([]Token{{Op: "and"}})

	b.ResetTimer()

	for range b.N {
		_, _ = compiler.Compile()
	}
}
