package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewConfigDefaults(t *testing.T) {
	Convey("Given NewConfig", t, func() {
		cfg := NewConfig()
		Convey("It should return non-nil config", func() {
			So(cfg, ShouldNotBeNil)
		})
		Convey("SchedulingTimeout should default to 10s", func() {
			So(cfg.SchedulingTimeout, ShouldEqual, 10*time.Second)
		})
		Convey("DependencyAwaitTimeout should have expected default", func() {
			So(cfg.DependencyAwaitTimeout, ShouldEqual, time.Second)
		})
	})
}

func BenchmarkNewConfig(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewConfig()
	}
}
