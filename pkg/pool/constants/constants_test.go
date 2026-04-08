package constants

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestConstantsCacheLinePadSize(t *testing.T) {
	t.Parallel()

	Convey("CacheLinePadSize is a realistic cache line", t, func() {
		So(CacheLinePadSize, ShouldBeGreaterThanOrEqualTo, 32)
		So(CacheLinePadSize&(CacheLinePadSize-1), ShouldEqual, 0)
	})
}
