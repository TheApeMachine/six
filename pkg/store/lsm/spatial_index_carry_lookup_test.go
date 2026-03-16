package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestSpatialIndexLookupRetainsCarryForwardState(t *testing.T) {
	gc.Convey("Given lookup requests flowing through the spatial index", t, func() {
		idx := buildPhaseIndex([]byte("Roy is in the Kitchen sink"))

		gc.Convey("the first lookup should seed persistent prompt memory", func() {
			results, _, _ := idx.LookupByPhase([]byte("Roy is in the Kit"))
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(idx.promptWavefront, gc.ShouldNotBeNil)
			gc.So(len(idx.promptWavefront.carryFrames), gc.ShouldEqual, 1)
		})

		gc.Convey("a follow-up lookup should be able to reuse carry-forward seeds", func() {
			_, _, _ = idx.LookupByPhase([]byte("Roy is in the Kit"))

			gc.So(idx.promptWavefront, gc.ShouldNotBeNil)
			seeds := idx.promptWavefront.seedCarryForward([]byte("Kitchen sink"))
			gc.So(len(seeds), gc.ShouldBeGreaterThan, 0)

			results, _, _ := idx.LookupByPhase([]byte("Kitchen"))
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(string(results[0]), gc.ShouldContainSubstring, "sink")
			gc.So(len(idx.promptWavefront.carryFrames), gc.ShouldBeGreaterThanOrEqualTo, 2)
		})
	})
}
