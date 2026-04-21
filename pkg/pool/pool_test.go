package pool

import (
	"sync"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewPool(t *testing.T) {
	t.Parallel()

	Convey("NewPool returns a pool with the configured max size", t, func() {
		So(NewPool(8), ShouldNotBeNil)
	})
}

func TestNewPoolWithFunc(t *testing.T) {
	t.Parallel()

	Convey("NewPoolWithFunc wires the task closure", t, func() {
		So(NewPoolWithFunc(2, func(int) {}), ShouldNotBeNil)
	})
}

func TestPoolWithFuncInvoke(t *testing.T) {
	Convey("Invoke delivers values to the shared task", t, func() {
		var sum atomic.Int32
		var wait sync.WaitGroup

		workerPool := NewPoolWithFunc(4, func(n int) {
			sum.Add(int32(n))
			wait.Done()
		})

		for value := range 32 {
			wait.Add(1)

			workerPool.Invoke(value)
		}

		wait.Wait()

		expected := int32(31 * 32 / 2)

		So(sum.Load(), ShouldEqual, expected)
	})
}

func BenchmarkPoolWithFuncInvoke(b *testing.B) {
	var wait sync.WaitGroup

	workerPool := NewPoolWithFunc(uint64(max(4, b.N/100+1)), func(int) {
		wait.Done()
	})

	b.ResetTimer()

	for b.Loop() {
		wait.Add(1)

		workerPool.Invoke(1)
	}

	wait.Wait()
}
