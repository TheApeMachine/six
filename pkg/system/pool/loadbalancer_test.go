package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewLoadBalancer(t *testing.T) {
	Convey("Given a LoadBalancer with 4 workers", t, func() {
		lb := NewLoadBalancer(4, 10)
		Convey("It should select a valid worker", func() {
			So(lb, ShouldNotBeNil)
			w, err := lb.SelectWorker()
			So(err, ShouldBeNil)
			So(w, ShouldBeIn, 0, 1, 2, 3)
		})
	})
}

func TestLoadBalancerSelectWorker(t *testing.T) {
	Convey("SelectWorker behavior", t, func() {
		Convey("It returns lowest-load worker", func() {
			lb := NewLoadBalancer(3, 5)
			lb.RecordJobStart(0)
			lb.RecordJobStart(0)
			w, err := lb.SelectWorker()
			So(err, ShouldBeNil)
			So(w, ShouldNotEqual, 0)
		})
		Convey("It returns ErrNoAvailableWorkers when all at capacity", func() {
			lb := NewLoadBalancer(2, 1)
			lb.RecordJobStart(0)
			lb.RecordJobStart(1)
			_, err := lb.SelectWorker()
			So(err, ShouldEqual, ErrNoAvailableWorkers)
		})
		Convey("It prefers lower latency when loads equal", func() {
			lb := NewLoadBalancer(2, 5)
			lb.RecordJobComplete(0, 100*time.Millisecond)
			lb.RecordJobComplete(1, 10*time.Millisecond)
			w, err := lb.SelectWorker()
			So(err, ShouldBeNil)
			So(w, ShouldEqual, 1)
		})
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

func BenchmarkLoadBalancerSelectWorker(b *testing.B) {
	lb := NewLoadBalancer(16, 100)
	for i := 0; i < 8; i++ {
		lb.RecordJobStart(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb.SelectWorker()
	}
}

func BenchmarkLoadBalancerRecordJobComplete(b *testing.B) {
	lb := NewLoadBalancer(16, 100)
	for i := 0; i < 8; i++ {
		lb.RecordJobStart(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		workerID := i % 16
		lb.RecordJobComplete(workerID, 10*time.Millisecond)
		lb.RecordJobStart(workerID)
	}
}
