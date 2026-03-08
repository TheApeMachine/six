package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestScalerError(t *testing.T) {
	Convey("Given ScalerError", t, func() {
		Convey("When ErrBadMaxWorkerConfig is used", func() {
			err := ErrBadMaxWorkerConfig

			Convey("Then Error returns the message", func() {
				So(err.Error(), ShouldEqual, "max workers config is bad")
			})
		})
	})
}

func TestScalerBootstrap(t *testing.T) {
	Convey("Given a new pool", t, func() {
		p := NewPool()
		defer p.Close()

		Convey("When the scaler has had time to bootstrap", func() {
			time.Sleep(poolBootstrapWait)

			Convey("Then at least one worker is active", func() {
				So(p.WorkerCount(), ShouldBeGreaterThanOrEqualTo, 1)
			})
		})
	})
}

func TestScalerWithSustainedLoad(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When sustained load is applied", func() {
			for i := 0; i < 30; i++ {
				done := make(chan struct{})
				p.Do(func() {
					time.Sleep(20 * time.Millisecond)
					close(done)
				})
				<-done
			}

			time.Sleep(400 * time.Millisecond)

			Convey("Then scaler may add workers for throughput", func() {
				workers := p.WorkerCount()
				So(workers, ShouldBeGreaterThanOrEqualTo, 1)
				So(workers, ShouldBeLessThanOrEqualTo, 1024)
			})
		})
	})
}

// --- Benchmarks ---

func BenchmarkScalerCycle(b *testing.B) {
	p := NewPool()
	defer p.Close()

	time.Sleep(poolBootstrapWait)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.WorkerCount()
		_ = p.TotalLatency()
	}
}
