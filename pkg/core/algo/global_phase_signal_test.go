package algo

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
)

func TestParseGlobalPhaseIndex(t *testing.T) {
	t.Parallel()

	Convey("non-finite values are inactive", t, func() {
		_, active := ParseGlobalPhaseIndex(math.NaN())

		So(active, ShouldBeFalse)

		_, active = ParseGlobalPhaseIndex(math.Inf(1))

		So(active, ShouldBeFalse)

		_, active = ParseGlobalPhaseIndex(math.Inf(-1))

		So(active, ShouldBeFalse)
	})

	Convey("strictly negative rounded values report lane -1", t, func() {
		lane, active := ParseGlobalPhaseIndex(-1.1)

		So(active, ShouldBeTrue)
		So(lane, ShouldEqual, -1)
	})

	Convey("large finite values reduce modulo PhaseWidth", t, func() {
		lane, active := ParseGlobalPhaseIndex(float64(7*gf.PhaseWidth + 3))

		So(active, ShouldBeTrue)
		So(lane, ShouldEqual, 3)
	})
}
