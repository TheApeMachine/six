package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

var sinkPoolValue *PoolValue[any]

func TestNewPoolValue(t *testing.T) {
	Convey("Given NewPoolValue", t, func() {
		Convey("When called with no opts", func() {
			pv := NewPoolValue[any]()
			Convey("It should return non-nil with empty Key and nil Value", func() {
				So(pv, ShouldNotBeNil)
				So(pv.Key, ShouldEqual, "")
				So(pv.Value, ShouldBeNil)
			})
		})
		Convey("When called with WithKey", func() {
			pv := NewPoolValue(WithKey[any]("state"))
			Convey("It should set Key", func() {
				So(pv.Key, ShouldEqual, "state")
			})
		})
		Convey("When called with WithValue", func() {
			pv := NewPoolValue(WithValue(42))
			Convey("It should set Value", func() {
				So(pv.Value, ShouldEqual, 42)
			})
		})
		Convey("When called with both opts", func() {
			snap := "snapshot"
			pv := NewPoolValue(WithKey[string]("state"), WithValue(snap))
			Convey("It should set Key and Value", func() {
				So(pv.Key, ShouldEqual, "state")
				So(pv.Value, ShouldEqual, snap)
			})
		})
	})
}

func BenchmarkNewPoolValue(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkPoolValue = NewPoolValue[any]()
	}
}

func BenchmarkNewPoolValue_WithOptions(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sinkPoolValue = NewPoolValue(WithKey[any]("key"), WithValue[any]("value"))
	}
}

func TestPoolValueTTL(t *testing.T) {
	Convey("PoolValue TTL can be set", t, func() {
		Convey("When set via direct field", func() {
			pv := &PoolValue[any]{TTL: time.Second}
			So(pv.TTL, ShouldEqual, time.Second)
		})
		Convey("When set via WithPoolValueTTL option", func() {
			pv := NewPoolValue(WithPoolValueTTL[any](5 * time.Minute))
			So(pv.TTL, ShouldEqual, 5*time.Minute)
		})
	})
}
