package cluster

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRead(t *testing.T) {
	Convey("Given a ControlPlane", t, func() {
		controlPlane := NewControlPlane()
		So(controlPlane, ShouldNotBeNil)
	})
}
