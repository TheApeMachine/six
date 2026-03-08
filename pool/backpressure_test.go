package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewBackPressureRegulator(t *testing.T) {
	Convey("NewBackPressureRegulator creates regulator", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		So(bp, ShouldNotBeNil)
		So(bp.GetPressure(), ShouldEqual, 0)
	})
}

func TestBackPressureRegulatorObserve(t *testing.T) {
	Convey("Observe updates metrics", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		bp.Observe(&Metrics{JobQueueSize: 80, AverageJobLatency: 100 * time.Millisecond})
		So(bp.GetPressure(), ShouldBeGreaterThan, 0.5)
	})
}

func TestBackPressureRegulatorLimit(t *testing.T) {
	Convey("Limit returns false when pressure < 0.8", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		bp.Observe(&Metrics{JobQueueSize: 50, AverageJobLatency: 10 * time.Millisecond})
		So(bp.Limit(), ShouldBeFalse)
	})
	Convey("Limit returns true when pressure >= 0.8", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		bp.Observe(&Metrics{JobQueueSize: 100, AverageJobLatency: 100 * time.Millisecond})
		So(bp.Limit(), ShouldBeTrue)
	})
}

func TestBackPressureRegulatorRenormalize(t *testing.T) {
	Convey("Renormalize decreases pressure when queue low and latency ok", t, func() {
		bp := NewBackPressureRegulator(100, 50*time.Millisecond, time.Second)
		bp.Observe(&Metrics{JobQueueSize: 100, AverageJobLatency: 100 * time.Millisecond})
		So(bp.Limit(), ShouldBeTrue)
		bp.Observe(&Metrics{JobQueueSize: 30, AverageJobLatency: 10 * time.Millisecond})
		bp.Renormalize()
		So(bp.GetPressure(), ShouldBeLessThan, 1.0)
	})
}
