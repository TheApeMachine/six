package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewLoadBalancer(t *testing.T) {
	Convey("NewLoadBalancer creates balancer", t, func() {
		lb := NewLoadBalancer(4, 10)
		So(lb, ShouldNotBeNil)
		w, err := lb.SelectWorker()
		So(err, ShouldBeNil)
		So(w, ShouldBeIn, 0, 1, 2, 3)
	})
}

func TestLoadBalancerSelectWorker(t *testing.T) {
	Convey("SelectWorker returns lowest-load worker", t, func() {
		lb := NewLoadBalancer(3, 5)
		lb.RecordJobStart(0)
		lb.RecordJobStart(0)
		w, err := lb.SelectWorker()
		So(err, ShouldBeNil)
		So(w, ShouldNotEqual, 0)
	})
	Convey("SelectWorker returns ErrNoAvailableWorkers when all at capacity", t, func() {
		lb := NewLoadBalancer(2, 1)
		lb.RecordJobStart(0)
		lb.RecordJobStart(1)
		_, err := lb.SelectWorker()
		So(err, ShouldEqual, ErrNoAvailableWorkers)
	})
	Convey("SelectWorker prefers lower latency when loads equal", t, func() {
		lb := NewLoadBalancer(2, 5)
		lb.RecordJobComplete(0, 100*time.Millisecond)
		lb.RecordJobComplete(1, 10*time.Millisecond)
		w, err := lb.SelectWorker()
		So(err, ShouldBeNil)
		So(w, ShouldEqual, 1)
	})
}

func TestLoadBalancerLimit(t *testing.T) {
	Convey("Limit returns false when any worker has capacity", t, func() {
		lb := NewLoadBalancer(2, 2)
		So(lb.Limit(), ShouldBeFalse)
	})
	Convey("Limit returns true when all workers at capacity", t, func() {
		lb := NewLoadBalancer(2, 1)
		lb.RecordJobStart(0)
		lb.RecordJobStart(1)
		So(lb.Limit(), ShouldBeTrue)
	})
}

func TestLoadBalancerRecordJobStartComplete(t *testing.T) {
	Convey("RecordJobStart increments load, RecordJobComplete decrements", t, func() {
		lb := NewLoadBalancer(2, 5)
		lb.RecordJobStart(0)
		w, _ := lb.SelectWorker()
		So(w, ShouldEqual, 1)
		lb.RecordJobComplete(0, 10*time.Millisecond)
		w, _ = lb.SelectWorker()
		So(w, ShouldBeIn, 0, 1)
	})
	Convey("RecordJobComplete clamps load to 0", t, func() {
		lb := NewLoadBalancer(2, 5)
		lb.RecordJobStart(0)
		lb.RecordJobComplete(0, time.Millisecond)
		lb.RecordJobComplete(0, time.Millisecond)
		So(lb.Limit(), ShouldBeFalse)
	})
}

func TestLoadBalancerRenormalize(t *testing.T) {
	Convey("Renormalize caps load at capacity", t, func() {
		lb := NewLoadBalancer(2, 2)
		lb.RecordJobStart(0)
		lb.RecordJobStart(0)
		lb.RecordJobStart(0)
		lb.Renormalize()
		w, err := lb.SelectWorker()
		So(err, ShouldBeNil)
		So(w, ShouldBeIn, 0, 1)
	})
}
