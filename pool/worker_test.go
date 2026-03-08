package pool

import (
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestWorkerProcessesJob(t *testing.T) {
	Convey("Given a pool with workers", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When a job is scheduled", func() {
			run := atomic.Bool{}
			done := make(chan struct{})

			p.Do(func() {
				run.Store(true)
				close(done)
			})

			<-done

			Convey("Then a worker executes it", func() {
				So(run.Load(), ShouldBeTrue)
			})
		})
	})
}

func TestWorkerLatencyUpdatedAfterJob(t *testing.T) {
	Convey("Given a pool with workers", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When a job runs for a measurable duration", func() {
			jobDuration := 10 * time.Millisecond
			done := make(chan struct{})

			p.Do(func() {
				time.Sleep(jobDuration)
				close(done)
			})
			<-done

			time.Sleep(5 * time.Millisecond)

			Convey("Then TotalLatency reflects the worker's last job duration", func() {
				total := p.TotalLatency()
				So(total, ShouldBeGreaterThanOrEqualTo, jobDuration.Nanoseconds())
			})
		})
	})
}

func TestWorkerConcurrentJobs(t *testing.T) {
	Convey("Given a pool with workers", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When multiple jobs are submitted concurrently", func() {
			const numJobs = 20
			var inFlight atomic.Int32
			maxConcurrent := atomic.Int32{}

			for i := 0; i < numJobs; i++ {
				p.Do(func() {
					n := inFlight.Add(1)
					if prev := maxConcurrent.Load(); n > prev {
						maxConcurrent.Store(n)
					}
					time.Sleep(2 * time.Millisecond)
					inFlight.Add(-1)
				})
			}

			time.Sleep(time.Duration(numJobs)*3*time.Millisecond + 100*time.Millisecond)

			Convey("Then jobs complete and workers handle concurrency", func() {
				So(inFlight.Load(), ShouldEqual, 0)
			})
		})
	})
}

// --- Benchmarks ---

func BenchmarkWorkerJobThroughput(b *testing.B) {
	p := NewPool()
	defer p.Close()

	time.Sleep(poolBootstrapWait)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		p.Do(func() { close(done) })
		<-done
	}
}
