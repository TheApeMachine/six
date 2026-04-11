package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestFrame_writeIntoProgramRegion(t *testing.T) {
	original := *core.Cfg
	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given a Frame with program words and a Value", t, func() {
		start := core.Cfg.Value.Region.Program.Start
		var frame Frame
		frame.Program[0] = 0x6
		frame.Program[1] = 0x6666666666666666

		var value primitive.Value

		Convey("writeIntoProgramRegion should copy into the configured program band", func() {
			frame.writeIntoProgramRegion(&value)

			So(value[start+0], ShouldEqual, frame.Program[0])
			So(value[start+1], ShouldEqual, frame.Program[1])
		})
	})

	Convey("Given a nil Frame", t, func() {
		var value primitive.Value

		Convey("writeIntoProgramRegion should not panic", func() {
			var frame *Frame
			frame.writeIntoProgramRegion(&value)
		})
	})

	Convey("Given a nil Value", t, func() {
		var frame Frame

		Convey("writeIntoProgramRegion should not panic", func() {
			frame.writeIntoProgramRegion(nil)
		})
	})
}

func BenchmarkFrame_writeIntoProgramRegion(b *testing.B) {
	original := *core.Cfg
	b.Cleanup(func() {
		*core.Cfg = original
	})

	var frame Frame
	frame.Program[0] = 0xF
	var value primitive.Value

	b.ResetTimer()

	for range b.N {
		frame.writeIntoProgramRegion(&value)
	}
}
