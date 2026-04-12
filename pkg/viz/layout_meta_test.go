package viz

import (
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestApplyVizLayoutDeterministic(t *testing.T) {
	Convey("same scatterKey yields identical viz_lx/viz_ly", t, func() {
		a := newEventWithMaps(EventTokenizerEmit, "tokenizer")
		applyVizLayout(&a, "tokenizer_emit", vizBandTokenizerVal, "a1b2c3")

		b := newEventWithMaps(EventTokenizerEmit, "tokenizer")
		applyVizLayout(&b, "tokenizer_emit", vizBandTokenizerVal, "a1b2c3")

		So(a.Meta[metaVizLX], ShouldEqual, b.Meta[metaVizLX])
		So(a.Meta[metaVizLY], ShouldEqual, b.Meta[metaVizLY])
		So(a.Meta[metaVizBand], ShouldEqual, b.Meta[metaVizBand])
	})

	Convey("viz coordinates stay in [0,1]", t, func() {
		ev := newEventWithMaps(EventQueueSubmit, "queue")
		applyVizLayoutQueue(&ev, 42, 0xfeed)

		lx, err := strconv.ParseFloat(ev.Meta[metaVizLX], 64)
		So(err, ShouldBeNil)
		ly, err2 := strconv.ParseFloat(ev.Meta[metaVizLY], 64)
		So(err2, ShouldBeNil)

		So(lx, ShouldBeGreaterThanOrEqualTo, 0)
		So(lx, ShouldBeLessThanOrEqualTo, 1)
		So(ly, ShouldBeGreaterThanOrEqualTo, 0)
		So(ly, ShouldBeLessThanOrEqualTo, 1)
	})
}
