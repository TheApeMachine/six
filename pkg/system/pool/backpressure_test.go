package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewBackPressureRegulator(t *testing.T) {
	Convey("Given a new back pressure regulator", t, func() {
		Convey("It should create a regulator with zero pressure", func() {
			bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
			So(bp, ShouldNotBeNil)
			So(bp.GetPressure(), ShouldEqual, 0)
		})
	})
}

func TestBackPressureRegulatorObserve(t *testing.T) {
	Convey("Given a BackPressureRegulator", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)

		Convey("It should update metrics", func() {
			bp.Observe(&Metrics{JobQueueSize: 80, AverageJobLatency: 100 * time.Millisecond})
			So(bp.GetPressure(), ShouldBeGreaterThan, 0.5)
		})
	})
}

func TestBackPressureRegulatorLimit(t *testing.T) {
	Convey("Given a BackPressureRegulator", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)

		Convey("It returns false when pressure < 0.8", func() {
			bp.Observe(&Metrics{JobQueueSize: 50, AverageJobLatency: 10 * time.Millisecond})
			So(bp.Limit(), ShouldBeFalse)
		})

		Convey("It returns true when pressure >= 0.8", func() {
			bp.Observe(&Metrics{JobQueueSize: 100, AverageJobLatency: 100 * time.Millisecond})
			So(bp.Limit(), ShouldBeTrue)
		})
	})
}

func TestBackPressureRegulatorRenormalize(t *testing.T) {
	Convey("Renormalize decreases pressure when queue low and latency ok", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		bp.Observe(&Metrics{JobQueueSize: 100, AverageJobLatency: 100 * time.Millisecond})
		So(bp.Limit(), ShouldBeTrue)
		bp.Observe(&Metrics{JobQueueSize: 30, AverageJobLatency: 10 * time.Millisecond})
		bp.Renormalize()
		So(bp.GetPressure(), ShouldBeLessThan, 0.8)
	})
}

func BenchmarkBackPressureRegulatorObserve(b *testing.B) {
	bp := NewBackPressureRegulator(1000, 50*time.Millisecond, time.Second)
	metrics := &Metrics{JobQueueSize: 500, AverageJobLatency: 25 * time.Millisecond}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bp.Observe(metrics)
	}
}

func BenchmarkBackPressureRegulatorLimit(b *testing.B) {
	bp := NewBackPressureRegulator(1000, 50*time.Millisecond, time.Second)
	metrics := &Metrics{JobQueueSize: 800, AverageJobLatency: 60 * time.Millisecond}
	bp.Observe(metrics)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bp.Limit()
	}
}

func BenchmarkBackPressureRegulatorGetPressure(b *testing.B) {
	bp := NewBackPressureRegulator(1000, 50*time.Millisecond, time.Second)
	bp.Observe(&Metrics{JobQueueSize: 600, AverageJobLatency: 40 * time.Millisecond})
	b.ReportAllocs()
	for b.Loop() {
		_ = bp.GetPressure()
	}
}

func BenchmarkBackPressureRegulatorRenormalize(b *testing.B) {
	bp := NewBackPressureRegulator(1000, 50*time.Millisecond, time.Second)
	bp.Observe(&Metrics{JobQueueSize: 900, AverageJobLatency: 100 * time.Millisecond})
	bp.Observe(&Metrics{JobQueueSize: 100, AverageJobLatency: 10 * time.Millisecond})
	b.ReportAllocs()
	for b.Loop() {
		bp.Renormalize()
	}
}
