package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestSpatialIndexServerCloseIsIdempotent(t *testing.T) {
	gc.Convey("Given a spatial index with an active RPC client", t, func() {
		idx := NewSpatialIndexServer()
		_ = idx.Client("test")

		gc.Convey("Close should shut the pipes down without panicking, even twice", func() {
			gc.So(idx.Close(), gc.ShouldBeNil)
			gc.So(idx.Close(), gc.ShouldBeNil)
		})
	})
}


