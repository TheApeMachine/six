package pool

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestScalerEvaluateScaleUpUnderQueueLoad(t *testing.T) {
	Convey("Given a pool scaler observing a deep queue", t, func() {
		p := New(context.Background(), 2, 6, NewConfig())
		defer p.Close()

		startLen := len(p.workerList)
		So(startLen, ShouldEqual, 2)

		p.metrics.mu.Lock()
		p.metrics.WorkerCount = 2
		p.metrics.JobQueueSize = 24
		p.metrics.mu.Unlock()

		sc := p.scaler
		sc.lastScale = time.Time{}
		sc.cooldown = 0
		sc.evaluate()

		p.workerMu.Lock()
		n := len(p.workerList)
		p.workerMu.Unlock()

		Convey("Workers should be added up to maxWorkers cap", func() {
			So(n, ShouldBeGreaterThan, startLen)
			So(n, ShouldBeLessThanOrEqualTo, 6)
		})
	})
}

func TestScalerEvaluateScaleDownWhenIdle(t *testing.T) {
	Convey("Given inflated worker metrics with an empty queue", t, func() {
		p := New(context.Background(), 2, 8, NewConfig())
		defer p.Close()

		So(len(p.workerList), ShouldEqual, 2)

		p.metrics.mu.Lock()
		p.metrics.WorkerCount = 5
		p.metrics.JobQueueSize = 0
		p.metrics.mu.Unlock()

		sc := p.scaler
		sc.lastScale = time.Time{}
		sc.cooldown = 0
		sc.evaluate()

		p.workerMu.Lock()
		n := len(p.workerList)
		p.workerMu.Unlock()

		Convey("At least one worker should be shed while staying above zero", func() {
			So(n, ShouldBeLessThan, 2)
			So(n, ShouldBeGreaterThanOrEqualTo, 1)
		})
	})
}

func TestScalerScaleUpWhenWorkerCountMetricZero(t *testing.T) {
	Convey("Given zero reported workers but pending queue depth", t, func() {
		p := New(context.Background(), 0, 3, NewConfig())
		defer p.Close()

		p.metrics.mu.Lock()
		p.metrics.WorkerCount = 0
		p.metrics.JobQueueSize = 3
		p.metrics.mu.Unlock()

		sc := p.scaler
		sc.lastScale = time.Time{}
		sc.cooldown = 0
		sc.evaluate()

		p.workerMu.Lock()
		n := len(p.workerList)
		p.workerMu.Unlock()

		Convey("Scaler should spawn workers", func() {
			So(n, ShouldBeGreaterThan, 0)
		})
	})
}
