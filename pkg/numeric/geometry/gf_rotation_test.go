package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGFRotation(t *testing.T) {
	Convey("Given a GFRotation", t, func() {
		Convey("It should hold CoordU and CoordV within GF(257) range", func() {
			rot := GFRotation{CoordU: 0, CoordV: 0}
			So(rot.CoordU, ShouldEqual, 0)
			So(rot.CoordV, ShouldEqual, 0)

			rot = GFRotation{CoordU: 256, CoordV: 128}
			So(rot.CoordU, ShouldEqual, 256)
			So(rot.CoordV, ShouldEqual, 128)
		})
	})
}
