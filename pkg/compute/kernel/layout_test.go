package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIsGeometricOpcode(t *testing.T) {
	Convey("Given raw opcode bytes", t, func() {
		Convey("It should report compose, sandwich and reverse opcodes as geometric", func() {
			So(IsGeometricOpcode(OpcodeGeometricCompose), ShouldBeTrue)
			So(IsGeometricOpcode(OpcodeGeometricSandwich), ShouldBeTrue)
			So(IsGeometricOpcode(OpcodeGeometricReverse), ShouldBeTrue)
		})

		Convey("It should not treat low-nibble truth-table opcodes as geometric", func() {
			So(IsGeometricOpcode(OpcodeXOR), ShouldBeFalse)
			So(IsGeometricOpcode(OpcodeRegionProgram), ShouldBeFalse)
		})
	})
}

func BenchmarkIsGeometricOpcode(b *testing.B) {
	for range b.N {
		_ = IsGeometricOpcode(OpcodeGeometricCompose)
	}
}
