package cpu

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

var benchSinkInstructionMeta uint32

func TestDecodeExtendedInstructionMeta(t *testing.T) {

	Convey("DecodeExtendedInstruction", t, func() {
		Convey("returns meta flag for bit 28", func() {
			instr := PackExtendedInstructionMeta(LGPXMemoryLoadMark, 3, 4, 0, true)
			op, argA, argB, argC, meta, ext := DecodeExtendedInstruction(instr)
			So(ext, ShouldBeTrue)
			So(meta, ShouldBeTrue)
			So(op, ShouldEqual, LGPXMemoryLoadMark)
			So(argA, ShouldEqual, 3)
			So(argB, ShouldEqual, 4)
			So(argC, ShouldEqual, 0)
		})

		Convey("clears meta when PackExtendedInstructionMeta meta is false", func() {
			instr := PackExtendedInstructionMeta(LGPXMemoryLoadMark, 9, 10, 11, false)
			op, argA, argB, argC, meta, ext := DecodeExtendedInstruction(instr)
			So(ext, ShouldBeTrue)
			So(meta, ShouldBeFalse)
			So(op, ShouldEqual, LGPXMemoryLoadMark)
			So(argA, ShouldEqual, 9)
			So(argB, ShouldEqual, 10)
			So(argC, ShouldEqual, 11)
		})
	})
}

func BenchmarkPackExtendedInstructionMeta(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		benchSinkInstructionMeta = PackExtendedInstructionMeta(
			LGPXResonatorUnbind,
			1, 2, 5,
			iteration%2 == 0,
		)
	}

	_ = benchSinkInstructionMeta
}
