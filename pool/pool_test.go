package pool

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

const poolBootstrapWait = 350 * time.Millisecond

func TestNewPool(t *testing.T) {
	Convey("Given NewPool", t, func() {
		Convey("When creating a new pool", func() {
			p := NewPool()
			defer p.Close()

			Convey("Then it returns a non-nil pool", func() {
				So(p, ShouldNotBeNil)
			})
			Convey("Then it has initialized channels", func() {
				So(p.workers, ShouldNotBeNil)
				So(p.jobs, ShouldNotBeNil)
			})
		})
	})
}

func TestPoolDo(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When Do is called with a job", func() {
			executed := atomic.Bool{}
			done := make(chan struct{})

			p.Do(func() {
				executed.Store(true)
				close(done)
			})

			Convey("Then the job executes", func() {
				select {
				case <-done:
					So(executed.Load(), ShouldBeTrue)
				case <-time.After(2 * time.Second):
					t.Fatal("job did not execute within 2s")
				}
			})
		})
	})
}

func TestPoolDoMany(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When many jobs are submitted", func() {
			const numJobs = 50
			var count atomic.Int32
			var wg sync.WaitGroup
			wg.Add(numJobs)

			for i := 0; i < numJobs; i++ {
				p.Do(func() {
					count.Add(1)
					wg.Done()
				})
			}

			wg.Wait()

			Convey("Then all jobs execute", func() {
				So(count.Load(), ShouldEqual, numJobs)
			})
		})
	})
}

func TestPoolClose(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()

		Convey("When Close is called", func() {
			p.Close()

			Convey("Then it does not panic", func() {
				So(func() { p.Close() }, ShouldNotPanic)
			})
			Convey("Then Do after close may block or panic - caller responsibility", func() {
				// Close cancels context; dispatch exits. Do(job) sends to unbuffered...
				// Actually jobs channel has buffer 10000. So Do might succeed but job never runs.
				// We just verify Close doesn't panic.
			})
		})
	})
}

func TestPoolWorkerCount(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()
		defer p.Close()

		Convey("When pool has bootstrapped", func() {
			time.Sleep(poolBootstrapWait)

			Convey("Then WorkerCount is at least 1", func() {
				So(p.WorkerCount(), ShouldBeGreaterThanOrEqualTo, 1)
			})
		})
	})
}

func TestPoolTotalLatency(t *testing.T) {
	Convey("Given a running pool", t, func() {
		p := NewPool()
		defer p.Close()

		time.Sleep(poolBootstrapWait)

		Convey("When a job that takes measurable time runs", func() {
			done := make(chan struct{})
			p.Do(func() {
				time.Sleep(5 * time.Millisecond)
				close(done)
			})
			<-done

			time.Sleep(10 * time.Millisecond)

			Convey("Then TotalLatency reflects execution time", func() {
				total := p.TotalLatency()
				So(total, ShouldBeGreaterThan, 0)
			})
		})
	})
}

// --- Benchmarks ---

func BenchmarkPoolDo(b *testing.B) {
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

func BenchmarkPoolDoParallel(b *testing.B) {
	p := NewPool()
	defer p.Close()

	time.Sleep(poolBootstrapWait)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done := make(chan struct{})
			p.Do(func() { close(done) })
			<-done
		}
	})
}
