package viz

import (
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestApplyVizLayoutDeterministic(t *testing.T) {
	Convey("Given same scatterKey", t, func() {
		a := NewEvent(EventTokenizerEmit, "tokenizer")
		applyVizLayout(&a, "tokenizer_emit", vizBandTokenizerVal, "a1b2c3")

		b := NewEvent(EventTokenizerEmit, "tokenizer")
		applyVizLayout(&b, "tokenizer_emit", vizBandTokenizerVal, "a1b2c3")

		Convey("It should yield identical viz_lx/viz_ly and viz_band", func() {
			So(a.Meta[metaVizLX], ShouldEqual, b.Meta[metaVizLX])
			So(a.Meta[metaVizLY], ShouldEqual, b.Meta[metaVizLY])
			So(a.Meta[metaVizBand], ShouldEqual, b.Meta[metaVizBand])
		})
	})

	Convey("Given a queue layout event", t, func() {
		ev := NewEvent(EventQueueSubmit, "queue")
		applyVizLayoutQueue(&ev, 42, 0xfeed)

		Convey("It should place viz_lx and viz_ly in [0,1]", func() {
			lx, err := strconv.ParseFloat(ev.Meta[metaVizLX], 64)
			So(err, ShouldBeNil)
			ly, err2 := strconv.ParseFloat(ev.Meta[metaVizLY], 64)
			So(err2, ShouldBeNil)

			So(lx, ShouldBeGreaterThanOrEqualTo, 0)
			So(lx, ShouldBeLessThanOrEqualTo, 1)
			So(ly, ShouldBeGreaterThanOrEqualTo, 0)
			So(ly, ShouldBeLessThanOrEqualTo, 1)
		})
	})
}

func BenchmarkApplyVizLayout(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ev := NewEvent(EventTokenizerEmit, "tokenizer")
		applyVizLayout(&ev, "tokenizer_emit", vizBandTokenizerVal, "a1b2c3")
	}
}

func BenchmarkApplyVizLayoutQueue(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ev := NewEvent(EventQueueSubmit, "queue")
		applyVizLayoutQueue(&ev, 42, 0xfeed)
	}
}
