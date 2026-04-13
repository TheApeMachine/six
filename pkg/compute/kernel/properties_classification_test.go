package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestLabelPropertiesWord(t *testing.T) {
	Convey("LabelPropertiesWord maps labels into Properties words", t, func() {
		Convey("empty label is zero", func() {
			So(LabelPropertiesWord(nil), ShouldEqual, 0)
			So(LabelPropertiesWord([]byte{}), ShouldEqual, 0)
		})

		Convey("decimal integers use GoldLabelWord", func() {
			So(LabelPropertiesWord([]byte("3")), ShouldEqual, GoldLabelWord(3))
			So(LabelPropertiesWord([]byte(" 42 ")), ShouldEqual, GoldLabelWord(42))
		})

		Convey("non-numeric bytes use a stable fingerprint", func() {
			a := LabelPropertiesWord([]byte("world"))
			b := LabelPropertiesWord([]byte("world"))
			c := LabelPropertiesWord([]byte("sports"))

			So(a, ShouldEqual, b)
			So(a, ShouldNotEqual, c)
		})
	})
}
