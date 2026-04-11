package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCPUCompiler_Compile(t *testing.T) {
	Convey("Given CPUCompiler with xor token", t, func() {
		compiler := NewCPUCompiler([]Token{{Op: "xor"}})

		Convey("Compile should produce one frame with xor nibble", func() {
			frames, err := compiler.Compile()

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Program[0], ShouldEqual, uint64(XOR&0xF))
		})
	})
}

func BenchmarkCPUCompiler_Compile(b *testing.B) {
	compiler := NewCPUCompiler([]Token{
		{Op: "xor"},
		{Op: "nand"},
	})

	b.ResetTimer()

	for range b.N {
		_, _ = compiler.Compile()
	}
}
