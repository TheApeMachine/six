package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIsGeometricOpcode(t *testing.T) {
	Convey("Given raw opcode bytes", t, func() {
		Convey("compose sandwich reverse should be geometric", func() {
			So(IsGeometricOpcode(OpcodeGeometricCompose), ShouldBeTrue)
			So(IsGeometricOpcode(OpcodeGeometricSandwich), ShouldBeTrue)
			So(IsGeometricOpcode(OpcodeGeometricReverse), ShouldBeTrue)
		})

		Convey("truth-table low nibbles should not be geometric", func() {
			So(IsGeometricOpcode(0x06), ShouldBeFalse)
			So(IsGeometricOpcode(0x40), ShouldBeFalse)
		})
	})
}

func BenchmarkIsGeometricOpcode(b *testing.B) {
	for range b.N {
		_ = IsGeometricOpcode(OpcodeGeometricCompose)
	}
}
