package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestDeriveOpcode_ConflictUsesAllRotationBands(t *testing.T) {
	Convey("Given conflicting geometric gates", t, func() {
		foundY := false
		foundZ := false

		for left := 0; left < 64; left++ {
			for right := left + 1; right < 96; right++ {
				opcode := DeriveOpcode(data.BaseChord(byte(left)), data.BaseChord(byte(right)))

				if opcode == OpRotateY {
					foundY = true
				}

				if opcode == OpRotateZ {
					foundZ = true
				}
			}
		}

		Convey("Conflict should no longer collapse onto RotateX only", func() {
			So(foundY, ShouldBeTrue)
			So(foundZ, ShouldBeTrue)
		})
	})
}
