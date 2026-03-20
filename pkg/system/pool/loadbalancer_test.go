package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewLoadBalancer(t *testing.T) {
	Convey("Given a LoadBalancer", t, func() {
		Convey("With 4 workers it should select a valid worker", func() {
			lb := NewLoadBalancer(4, 10)
			So(lb, ShouldNotBeNil)
			w, err := lb.SelectWorker()
			So(err, ShouldBeNil)
			So(w, ShouldBeIn, 0, 1, 2, 3)
		})
	})
}

func TestLoadBalancerSelectWorker(t *testing.T) {
	Convey("Given a LoadBalancer", t, func() {
		Convey("It should return the lowest-load worker", func() {
			lb := NewLoadBalancer(3, 5)
			lb.RecordJobStart(0)
			lb.RecordJobStart(0)
			w, err := lb.SelectWorker()
			So(err, ShouldBeNil)
			So(w, ShouldNotEqual, 0)
		})
		Convey("It should return ErrNoAvailableWorkers when all at capacity", func() {
			lb := NewLoadBalancer(2, 1)
			lb.RecordJobStart(0)
			lb.RecordJobStart(1)
			_, err := lb.SelectWorker()
			So(err, ShouldEqual, ErrNoAvailableWorkers)
		})
		Convey("It should prefer lower latency when loads equal", func() {
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

func TestLoadBalancerObserveAddsWorkersFromMetrics(t *testing.T) {
	Convey("Given a LoadBalancer sized for 2 workers", t, func() {
		lb := NewLoadBalancer(2, 4)
		m := &Metrics{WorkerCount: 5}

		Convey("Observe should expand internal maps", func() {
			lb.Observe(m)
			for id := 0; id < 5; id++ {
				_, ok := lb.workerCapacity[id]
				So(ok, ShouldBeTrue)
			}
			So(lb.Limit(), ShouldBeFalse)
		})
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
	b.ReportAllocs()
	for b.Loop() {
		lb.SelectWorker()
	}
}

func BenchmarkLoadBalancerRecordJobComplete(b *testing.B) {
	lb := NewLoadBalancer(16, 100)
	for i := 0; i < 8; i++ {
		lb.RecordJobStart(i)
	}
	var round int
	b.ReportAllocs()
	for b.Loop() {
		workerID := round % 16
		round++
		lb.RecordJobComplete(workerID, 10*time.Millisecond)
		lb.RecordJobStart(workerID)
	}
}

func BenchmarkLoadBalancerObserveAndLimit(b *testing.B) {
	lb := NewLoadBalancer(4, 8)
	metrics := &Metrics{WorkerCount: 8}
	lb.Observe(metrics)
	b.ReportAllocs()
	for b.Loop() {
		lb.Observe(metrics)
		_ = lb.Limit()
	}
}

func BenchmarkLoadBalancerRenormalize(b *testing.B) {
	lb := NewLoadBalancer(8, 4)
	for workerID := 0; workerID < 8; workerID++ {
		for range 8 {
			lb.RecordJobStart(workerID)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		lb.Renormalize()
	}
}
