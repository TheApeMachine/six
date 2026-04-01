package pysix

import (
	"os/exec"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func python3OK() bool {

	return exec.Command("python3", "--version").Run() == nil
}

func TestCompileSourceAdd(t *testing.T) {

	if !python3OK() {
		t.Skip("python3 not available")
	}

	convey.Convey("Given simple addition assignment", t, func() {
		src := "a = 19 + 23\n"
		prog, locals, err := CompileSource(src)

		convey.So(err, convey.ShouldBeNil)

		var frame [FrameWords]uint64

		convey.So(Run(&frame, prog), convey.ShouldBeNil)

		convey.So(frame[locals["a"]], convey.ShouldEqual, uint64(42))
	})
}

func TestCompileSourceLargeLiteral(t *testing.T) {

	if !python3OK() {
		t.Skip("python3 not available")
	}

	convey.Convey("Given literal wider than 16 bits", t, func() {
		src := "x = 65536 + 2\n"
		prog, locals, err := CompileSource(src)

		convey.So(err, convey.ShouldBeNil)

		var frame [FrameWords]uint64

		convey.So(Run(&frame, prog), convey.ShouldBeNil)

		convey.So(frame[locals["x"]], convey.ShouldEqual, uint64(65538))
	})
}

func TestCompileSourceIfEq(t *testing.T) {

	if !python3OK() {
		t.Skip("python3 not available")
	}

	convey.Convey("Given if/else with equality", t, func() {
		src := `a = 1
b = 2
if a == b:
    m = 10
else:
    m = 99
`
		prog, locals, err := CompileSource(src)

		convey.So(err, convey.ShouldBeNil)

		var frame [FrameWords]uint64

		convey.So(Run(&frame, prog), convey.ShouldBeNil)

		convey.So(frame[locals["m"]], convey.ShouldEqual, uint64(99))
	})
}

func TestCompileSourceForRange(t *testing.T) {

	if !python3OK() {
		t.Skip("python3 not available")
	}

	convey.Convey("Given for range accumulation", t, func() {
		src := `s = 0
i = 0
for i in range(4):
    s += i
`
		prog, locals, err := CompileSource(src)

		convey.So(err, convey.ShouldBeNil)

		var frame [FrameWords]uint64

		convey.So(Run(&frame, prog), convey.ShouldBeNil)

		convey.So(frame[locals["s"]], convey.ShouldEqual, uint64(0+1+2+3))
	})
}

func TestCompileSourceMultiply(t *testing.T) {

	if !python3OK() {
		t.Skip("python3 not available")
	}

	convey.Convey("Given multiplication by literal", t, func() {
		src := "a = 7 * 3\n"
		prog, locals, err := CompileSource(src)

		convey.So(err, convey.ShouldBeNil)

		var frame [FrameWords]uint64

		convey.So(Run(&frame, prog), convey.ShouldBeNil)

		convey.So(frame[locals["a"]], convey.ShouldEqual, uint64(21))
	})
}

func BenchmarkCompileSource(b *testing.B) {

	if !python3OK() {
		b.Skip("python3 not available")
	}

	src := "a = 1 + 2 * 3\n"

	b.ResetTimer()

	for k := 0; k < b.N; k++ {
		_, _, err := CompileSource(src)

		if err != nil {
			b.Fatal(err)
		}
	}
}
