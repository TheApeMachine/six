package pool

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAdaptiveScalerRegulatorLimitAtCapacity(t *testing.T) {
	Convey("Given an adaptive scaler at max workers with high queue depth", t, func() {
		pool := New(context.Background(), 2, 2, NewConfig())
		defer pool.Close()

		as := NewAdaptiveScalerRegulator(pool, 2, 2, &ScalerConfig{
			TargetLoad:         2,
			ScaleUpThreshold:   1,
			ScaleDownThreshold: 0.1,
			Cooldown:           time.Hour,
		})

		m := &Metrics{
			WorkerCount:  2,
			JobQueueSize: 100,
		}

		Convey("Limit should report limiting", func() {
			as.Observe(m)
			So(as.Limit(), ShouldBeTrue)
		})
	})
}

func TestAdaptiveScalerRegulatorRenormalizeAfterCooldown(t *testing.T) {
	Convey("Given an adaptive scaler with observed metrics", t, func() {
		pool := New(context.Background(), 2, 8, NewConfig())
		defer pool.Close()

		as := NewAdaptiveScalerRegulator(pool, 2, 8, &ScalerConfig{
			TargetLoad:         2,
			ScaleUpThreshold:   4,
			ScaleDownThreshold: 1,
			Cooldown:           0,
		})

		as.Observe(&Metrics{WorkerCount: 2, JobQueueSize: 12})

		Convey("Renormalize after cooldown should run another evaluation", func() {
			before := pool.WorkerCount()
			as.Renormalize()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			waitErr := pool.WaitForWorkerCount(ctx, before)
			So(waitErr, ShouldBeNil)

			after := pool.WorkerCount()
			So(after, ShouldBeGreaterThanOrEqualTo, before)
		})
	})
}

func TestAdaptiveScalerRegulatorLimitWithNilMetrics(t *testing.T) {
	Convey("Given an adaptive scaler without observed metrics", t, func() {
		pool := New(context.Background(), 1, 4, NewConfig())
		defer pool.Close()

		as := NewAdaptiveScalerRegulator(pool, 1, 4, &ScalerConfig{
			TargetLoad:         2,
			ScaleUpThreshold:   4,
			ScaleDownThreshold: 1,
			Cooldown:           time.Millisecond,
		})

		Convey("Limit should be false", func() {
			So(as.Limit(), ShouldBeFalse)
		})
	})
}

func TestAdaptiveScalerRegulatorEvaluateScaleDown(t *testing.T) {
	Convey("Given an adaptive scaler above its minimum worker count", t, func() {
		pool := New(context.Background(), 3, 4, NewConfig())
		defer pool.Close()

		as := NewAdaptiveScalerRegulator(pool, 1, 4, &ScalerConfig{
			TargetLoad:         2,
			ScaleUpThreshold:   4,
			ScaleDownThreshold: 1,
			Cooldown:           0,
		})

		as.Observe(pool.Metrics())

		before := pool.WorkerCount()

		Convey("evaluate should reduce workers when load is below threshold", func() {
			as.evaluate()

			after := pool.WorkerCount()

			So(before, ShouldBeGreaterThan, 1)
			So(after, ShouldEqual, before-1)
			So(pool.Metrics().WorkerCount, ShouldEqual, after)
		})
	})
}

func BenchmarkAdaptiveScalerObserveNoOpCooldown(b *testing.B) {
	pool := New(context.Background(), 1, 4, NewConfig())
	defer pool.Close()

	as := NewAdaptiveScalerRegulator(pool, 1, 4, &ScalerConfig{
		TargetLoad:         2,
		ScaleUpThreshold:   4,
		ScaleDownThreshold: 1,
		Cooldown:           time.Hour,
	})

	m := &Metrics{WorkerCount: 2, JobQueueSize: 1}
	as.Observe(m)
	b.ReportAllocs()
	for b.Loop() {
		as.Observe(m)
	}
}

func BenchmarkAdaptiveScalerLimitAtCapacity(b *testing.B) {
	pool := New(context.Background(), 2, 2, NewConfig())
	defer pool.Close()

	as := NewAdaptiveScalerRegulator(pool, 2, 2, &ScalerConfig{
		TargetLoad:         2,
		ScaleUpThreshold:   1,
		ScaleDownThreshold: 0.1,
		Cooldown:           time.Second,
	})

	as.metrics = &Metrics{
		WorkerCount:  2,
		JobQueueSize: 64,
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = as.Limit()
	}
}

func BenchmarkAdaptiveScalerRenormalizeNoScale(b *testing.B) {
	pool := New(context.Background(), 2, 4, NewConfig())
	defer pool.Close()

	as := NewAdaptiveScalerRegulator(pool, 2, 4, &ScalerConfig{
		TargetLoad:         2,
		ScaleUpThreshold:   8,
		ScaleDownThreshold: 0.1,
		Cooldown:           0,
	})

	as.metrics = &Metrics{
		WorkerCount:  2,
		JobQueueSize: 4,
	}

	b.ReportAllocs()
	for b.Loop() {
		as.Renormalize()
	}
}
