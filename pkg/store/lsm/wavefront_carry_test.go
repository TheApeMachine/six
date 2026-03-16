package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestWavefrontCarryForwardCache(t *testing.T) {
	gc.Convey("Given a wavefront with carry-forward enabled", t, func() {
		idx := buildPhaseIndex([]byte("Roy is in the Kitchen sink"))
		wf := NewWavefront(
			idx,
			WavefrontWithMaxHeads(64),
			WavefrontWithMaxDepth(64),
			WavefrontWithCarryForward(4, 3, 8),
		)

		gc.Convey("the first prompt should persist its terminal phase", func() {
			results := wf.SearchPrompt([]byte("Roy is in the Kit"), nil, nil)
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(len(wf.carryFrames), gc.ShouldEqual, 1)
			gc.So(wf.persistencePhase, gc.ShouldNotEqual, 0)
		})

		gc.Convey("a related next prompt should receive warm seeds from the cached tail", func() {
			_ = wf.SearchPrompt([]byte("Roy is in the Kit"), nil, nil)

			seeds := wf.seedCarryForward([]byte("Kitchen sink"))
			gc.So(len(seeds), gc.ShouldBeGreaterThan, 0)
			gc.So(seeds[0].promptIdx, gc.ShouldEqual, len("Kit"))
			gc.So(seeds[0].energy, gc.ShouldEqual, 0)

			results := wf.SearchPrompt([]byte("Kitchen sink"), nil, nil)
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)

			decoded := idx.decodeValues(results[0].Path)
			gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
			gc.So(string(decoded[0]), gc.ShouldContainSubstring, "Kitchen sink")
		})
	})
}
