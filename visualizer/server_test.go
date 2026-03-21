package visualizer

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestResolveUDPListenAddr verifies the visualizer does not hardcode its UDP bind
address inside ListenAndServe.
*/
func TestResolveUDPListenAddr(t *testing.T) {
	gc.Convey("Given no explicit UDP address", t, func() {
		udpAddr, err := resolveUDPListenAddr(":8257", "")
		gc.So(err, gc.ShouldBeNil)
		gc.So(udpAddr.String(), gc.ShouldEqual, "127.0.0.1:8258")
	})

	gc.Convey("Given an explicit UDP address", t, func() {
		udpAddr, err := resolveUDPListenAddr(":8257", "0.0.0.0:9000")
		gc.So(err, gc.ShouldBeNil)
		gc.So(udpAddr.String(), gc.ShouldEqual, "0.0.0.0:9000")
	})
}
