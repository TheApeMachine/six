package pool

import (
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestJob(t *testing.T) {
	Convey("Given a Job type", t, func() {
		Convey("When a Job is created and invoked", func() {
			executed := atomic.Bool{}

			var job Job = func() {
				executed.Store(true)
			}
			job()

			Convey("Then it executes", func() {
				So(executed.Load(), ShouldBeTrue)
			})
		})

		Convey("When a Job captures state", func() {
			accum := atomic.Int32{}

			job := func() {
				accum.Add(42)
			}
			job()
			job()

			Convey("Then closure works correctly", func() {
				So(accum.Load(), ShouldEqual, 84)
			})
		})
	})
}

// --- Benchmarks ---

func BenchmarkJobInvocation(b *testing.B) {
	var job Job = func() {}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job()
	}
}
