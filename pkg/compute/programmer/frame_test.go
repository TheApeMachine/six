package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestFrame_writeIntoProgramRegion checks that lowered words land in the Value program slab.
*/
func TestFrame_writeIntoProgramRegion(t *testing.T) {
	Convey("Given a minted Value and a frame with a marked program word", t, func() {
		values, err := primitive.NewValue([]byte("frame write"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]

		var frame Frame
		mark := uint64(0xC0FFEE)
		frame.Program[0] = mark

		Convey("writeIntoProgramRegion should copy into the configured program words", func() {
			frame.WriteIntoProgramRegion(value)

			start := core.Cfg.Value.Region.Program.Start

			So((*value)[start], ShouldEqual, mark)
		})

		Reset(func() {
			value.Close()
		})
	})

	Convey("Given nil frame or nil value", t, func() {
		var nilFrame *Frame
		var nilValue *primitive.Value
		var frame Frame

		Convey("writeIntoProgramRegion should not panic", func() {
			nilFrame.WriteIntoProgramRegion(&primitive.Value{})
			frame.WriteIntoProgramRegion(nilValue)
		})
	})
}

func BenchmarkFrame_writeIntoProgramRegion(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench frame"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	var frame Frame
	frame.Program[0] = 1

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		frame.WriteIntoProgramRegion(value)
	}
}
