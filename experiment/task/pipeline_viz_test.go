package task

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestResolveVizTelemetryEndpoint(t *testing.T) {
	Convey("Given a compatibility viz address", t, func() {
		previousCfg := core.Cfg
		core.Cfg = &core.Config{TelemetryEndpoint: "127.0.0.1:8258"}

		defer func() {
			core.Cfg = previousCfg
		}()

		Convey("It should keep the configured UDP port while targeting the bridge host", func() {
			So(resolveVizTelemetryEndpoint(":6600"), ShouldEqual, "127.0.0.1:8258")
			So(resolveVizTelemetryEndpoint("0.0.0.0:6600"), ShouldEqual, "127.0.0.1:8258")
			So(resolveVizTelemetryEndpoint("192.168.0.5:6600"), ShouldEqual, "192.168.0.5:8258")
		})

		Convey("It should fall back to the default bridge UDP port when config is unset", func() {
			core.Cfg.TelemetryEndpoint = ""

			So(resolveVizTelemetryEndpoint(":6600"), ShouldEqual, "127.0.0.1:8258")
			So(resolveVizTelemetryEndpoint("localhost:6600"), ShouldEqual, "localhost:8258")
		})
	})
}
