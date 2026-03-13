package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewPoolValue(t *testing.T) {
	Convey("NewPoolValue with no opts returns empty PoolValue", t, func() {
		pv := NewPoolValue[any]()
		So(pv, ShouldNotBeNil)
		So(pv.Key, ShouldEqual, "")
		So(pv.Value, ShouldBeNil)
	})
	Convey("NewPoolValue with WithKey sets Key", t, func() {
		pv := NewPoolValue(WithKey[any]("state"))
		So(pv.Key, ShouldEqual, "state")
	})
	Convey("NewPoolValue with WithValue sets Value", t, func() {
		pv := NewPoolValue(WithValue(42))
		So(pv.Value, ShouldEqual, 42)
	})
	Convey("NewPoolValue with both opts", t, func() {
		snap := "snapshot"
		pv := NewPoolValue(WithKey[string]("state"), WithValue(snap))
		So(pv.Key, ShouldEqual, "state")
		So(pv.Value, ShouldEqual, snap)
	})
}

func TestPoolValueTTL(t *testing.T) {
	Convey("PoolValue TTL can be set", t, func() {
		pv := &PoolValue[any]{TTL: time.Second}
		So(pv.TTL, ShouldEqual, time.Second)
	})
}
