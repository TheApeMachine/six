package core

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCompileFunc(t *testing.T) {
	Convey("CompileFunc", t, func() {
		Convey("parses OP SRC DST and packs like the CPU kernel", func() {
			p, err := CompileFunc("XOR 5 6")

			So(err, ShouldBeNil)
			So(p, ShouldResemble, []uint32{0x6 | 5<<4 | 6<<18})
		})

		Convey("treats NOP and HALT as zero slots", func() {
			p, err := CompileFunc("NOP\nHALT")

			So(err, ShouldBeNil)
			So(p, ShouldResemble, []uint32{0, 0})
		})

		Convey("accepts lowercase mnemonics", func() {
			p, err := CompileFunc("and 10 11")

			So(err, ShouldBeNil)
			So(p, ShouldResemble, []uint32{0x1 | 10<<4 | 11<<18})
		})

		Convey("accepts numeric truth-table op 0-15", func() {
			p, err := CompileFunc("0x6 1 2")

			So(err, ShouldBeNil)
			So(p, ShouldResemble, []uint32{0x6 | 1<<4 | 2<<18})
		})

		Convey("rejects word index > 127", func() {
			_, err := CompileFunc("XOR 128 0")

			So(err, ShouldNotBeNil)
		})

		Convey("rejects unknown opcode name", func() {
			_, err := CompileFunc("NOPE 0 0")

			So(err, ShouldNotBeNil)
		})
	})
}

func BenchmarkCompileFunc(b *testing.B) {
	src := "XOR 5 6\nCOPY 58 60\nNOP\n"

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = CompileFunc(src)
	}
}
