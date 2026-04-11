package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestInstallProgram(t *testing.T) {
	original := *core.Cfg

	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given a zero Value", t, func() {
		var value primitive.Value

		Convey("InstallProgram with NewProgram inline resolution should lower program words", func() {
			err := InstallProgram(&value, affinitySource)

			So(err, ShouldBeNil)
			So(value[kernel.ProgramRotTabWord], ShouldNotEqual, uint64(0))
			So(value[kernel.ProgramSrcAWord], ShouldEqual, kernel.PackRegionRef(0, 16))
		})

		Convey("InstallProgram with empty source should leave program region untouched", func() {
			var clean primitive.Value

			err := InstallProgram(&clean, "")

			So(err, ShouldBeNil)
			So(clean[kernel.ProgramRotTabWord], ShouldEqual, uint64(0))
		})
	})
}
