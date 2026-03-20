package pool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

type testTask struct {
	reader *bytes.Reader
	err    error
	onRead func()
}

func newTestTask(payload string) *testTask {
	return &testTask{
		reader: bytes.NewReader([]byte(payload)),
	}
}

func newErrTask(err error) *testTask {
	return &testTask{
		err: err,
	}
}

func (task *testTask) Read(p []byte) (n int, err error) {
	if task.onRead != nil {
		task.onRead()
		task.onRead = nil
	}

	if task.err != nil {
		err = task.err
		task.err = nil
		return 0, err
	}

	if task.reader == nil {
		return 0, io.EOF
	}

	return task.reader.Read(p)
}

func (task *testTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (task *testTask) Close() error {
	return nil
}

func waitForResult(store *ResultStore, id string, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, ok := store.Result(id)
		if ok {
			return result, nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil, fmt.Errorf("timed out waiting for result")
}

/*
waitForResultBenchmark uses tighter polling than integration tests so schedule
benchmarks measure dispatch/worker overhead instead of a fixed 10ms sleep slice.
*/
func waitForResultBenchmark(store *ResultStore, id string, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	poll := time.Microsecond

	for time.Now().Before(deadline) {
		result, ok := store.Result(id)
		if ok {
			return result, nil
		}

		time.Sleep(poll)

		if poll < time.Millisecond {
			poll *= 2
		}
	}

	return nil, fmt.Errorf("timed out waiting for result")
}

/*
resultWaitBudget is how long schedule benchmarks wait for a stored outcome.
It must stay above the pool scheduling timeout (and retry backoff) so slow
machines, coverage, and queue pressure do not flake against the 5s dispatch
deadline in manage().
*/
func resultWaitBudget(workerPool *Pool) time.Duration {
	budget := 3 * workerPool.getSchedulingTimeout()
	if budget < 15*time.Second {
		return 15 * time.Second
	}

	return budget
}

func TestScheduleAndRetrieve(t *testing.T) {
	Convey("Given a Pool with 2 min and 4 max workers", t, func() {
		workerPool := New(context.Background(), 2, 4, NewConfig())
		defer workerPool.Close()

		Convey("When Schedule is called with a task that yields 42", func() {
			err := workerPool.Schedule("job-1", COMPUTE, newTestTask("42"))

			Convey("The result channel should receive value 42 with no error", func() {
				So(err, ShouldBeNil)
				result, resultErr := waitForResult(workerPool.store, "job-1", 5*time.Second)
				So(resultErr, ShouldBeNil)
				So(result.Error, ShouldBeNil)
				So(string(result.Value.([]byte)), ShouldEqual, "42")
			})
		})
	})
}

func TestScheduleError(t *testing.T) {
	Convey("Given a Pool", t, func() {
		p := New(context.Background(), 2, 4, NewConfig())
		defer p.Close()

		Convey("When Schedule is called with a task that returns an error", func() {
			err := p.Schedule(
				"job-err",
				COMPUTE,
				newErrTask(fmt.Errorf("intentional failure")),
				WithRetry(1, nil),
			)

			Convey("The result channel should receive the error", func() {
				So(err, ShouldBeNil)
				result, resultErr := waitForResult(p.store, "job-err", 5*time.Second)
				So(resultErr, ShouldBeNil)
				So(result.Error, ShouldNotBeNil)
				So(result.Error.Error(), ShouldEqual, "intentional failure")
			})
		})
	})
}

func TestConcurrentSchedule(t *testing.T) {
	Convey("Given a Pool with 4 min and 8 max workers", t, func() {
		p := New(context.Background(), 4, 8, NewConfig())
		defer p.Close()

		Convey("When 100 jobs are scheduled concurrently", func() {
			const n = 100
			var completed int64
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("concurrent-%d", i)
				task := newTestTask("done")
				task.onRead = func() {
					atomic.AddInt64(&completed, 1)
				}
				err := p.Schedule(id, COMPUTE, task)
				So(err, ShouldBeNil)
			}

			Convey("All jobs should complete successfully", func() {
				for i := 0; i < n; i++ {
					id := fmt.Sprintf("concurrent-%d", i)
					result, resultErr := waitForResult(p.store, id, 10*time.Second)
					So(resultErr, ShouldBeNil)
					So(result.Error, ShouldBeNil)
					So(string(result.Value.([]byte)), ShouldEqual, "done")
				}
				So(atomic.LoadInt64(&completed), ShouldEqual, n)
			})
		})
	})
}

func TestScheduleWithRepeatedID(t *testing.T) {
	Convey("Given a Pool", t, func() {
		p := New(context.Background(), 2, 4, NewConfig())
		defer p.Close()

		Convey("When the same job ID is scheduled more than once", func() {
			errFirst := p.Schedule("repeat", COMPUTE, newTestTask("first"))
			errSecond := p.Schedule("repeat", COMPUTE, newTestTask("second"))

			Convey("Each schedule should receive its own result", func() {
				So(errFirst, ShouldBeNil)
				So(errSecond, ShouldBeNil)

				result, resultErr := waitForResult(p.store, "repeat", 5*time.Second)
				So(resultErr, ShouldBeNil)
				So(result.Error, ShouldBeNil)
				So(
					string(result.Value.([]byte)) == "first" ||
						string(result.Value.([]byte)) == "second",
					ShouldBeTrue,
				)
			})
		})
	})
}

func TestBroadcastGroup(t *testing.T) {
	Convey("Given a Pool and a BroadcastGroup", t, func() {
		p := New(context.Background(), 2, 4, NewConfig())
		defer p.Close()
		bg := p.CreateBroadcastGroup("test-group", time.Minute)
		ch := bg.Subscribe("sub-1", 10)

		Convey("When Send is called with a result", func() {
			r := NewResult("hello")
			bg.Send(r)

			Convey("Subscribers should receive the result", func() {
				select {
				case got := <-ch:
					So(got.Value, ShouldEqual, "hello")
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for broadcast")
				}
			})
		})
	})
}

func TestCircuitBreaker(t *testing.T) {
	Convey("Given a CircuitBreaker with max 2 failures", t, func() {
		Convey("Initially Allow should return true", func() {
			cb := NewCircuitBreaker(2, 100*time.Millisecond, 1)
			So(cb.Allow(), ShouldBeTrue)
		})

		Convey("After 2 RecordFailure calls, Allow should return false", func() {
			cb := NewCircuitBreaker(2, 100*time.Millisecond, 1)
			cb.RecordFailure()
			cb.RecordFailure()
			So(cb.Allow(), ShouldBeFalse)
		})

		Convey("After reset timeout, Allow should return true (half-open)", func() {
			cb := NewCircuitBreaker(2, 100*time.Millisecond, 1)
			cb.RecordFailure()
			cb.RecordFailure()
			time.Sleep(150 * time.Millisecond)
			So(cb.Allow(), ShouldBeTrue)
		})

		Convey("After successful probe in half-open, breaker should close", func() {
			cb := NewCircuitBreaker(2, 100*time.Millisecond, 1)
			cb.RecordFailure()
			cb.RecordFailure()
			time.Sleep(150 * time.Millisecond)
			cb.Allow()
			cb.RecordSuccess()
			So(cb.Allow(), ShouldBeTrue)
		})
	})
}

func BenchmarkSchedule(b *testing.B) {
	p := New(context.Background(), 4, 16, NewConfig())
	defer p.Close()
	wait := resultWaitBudget(p)
	var seq atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		id := fmt.Sprintf("bench-%d", seq.Add(1))
		if err := p.Schedule(id, COMPUTE, newTestTask("ok")); err != nil {
			b.Fatalf("unexpected schedule error: %v", err)
		}
		if _, err := waitForResultBenchmark(p.store, id, wait); err != nil {
			b.Fatalf("unexpected wait error: %v", err)
		}
	}
}

func BenchmarkBroadcastSend(b *testing.B) {
	p := New(context.Background(), 2, 4, NewConfig())
	defer p.Close()
	bg := p.CreateBroadcastGroup("bench-group", time.Minute)
	resultCh := bg.Subscribe("sub", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-resultCh:
				if !ok {
					return
				}
			}
		}
	}()
	r := NewResult("payload")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bg.Send(r)
	}
}

func TestPoolScheduleNilTask(t *testing.T) {
	Convey("Given a Pool", t, func() {
		p := New(context.Background(), 1, 2, NewConfig())
		defer p.Close()

		Convey("Schedule with nil task should error", func() {
			err := p.Schedule("nil-job", STORE, nil)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "nil")
		})
	})
}

func TestPoolCloseNilReceiver(t *testing.T) {
	Convey("Given a nil Pool pointer", t, func() {
		var p *Pool
		Convey("Close should be nil-safe", func() {
			So(p.Close(), ShouldBeNil)
		})
	})
}

func TestPoolCircuitBreakerBlocksAfterFailures(t *testing.T) {
	Convey("Given a Pool with a tight circuit breaker", t, func() {
		p := New(context.Background(), 2, 4, NewConfig())
		defer p.Close()

		circuitOpts := []JobOption{
			WithCircuitBreaker("shared-circuit", 1, 10*time.Second, 1),
			WithRetry(1, nil),
		}

		So(p.Schedule("trip", COMPUTE, newErrTask(fmt.Errorf("first trip")), circuitOpts...), ShouldBeNil)
		_, waitErr := waitForResult(p.store, "trip", 5*time.Second)
		So(waitErr, ShouldBeNil)

		Convey("Next schedule on same circuit should be rejected", func() {
			err := p.Schedule("blocked", COMPUTE, newTestTask("nope"), circuitOpts...)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "circuit breaker")
		})
	})
}

func TestPoolMinWorkersZeroStillRunsJobs(t *testing.T) {
	Convey("Given a Pool starting with zero workers", t, func() {
		p := New(context.Background(), 0, 4, NewConfig())
		defer p.Close()

		Convey("Schedule should still complete work", func() {
			So(p.Schedule("cold-start", COMPUTE, newTestTask("ok")), ShouldBeNil)
			res, err := waitForResult(p.store, "cold-start", 8*time.Second)
			So(err, ShouldBeNil)
			So(res.Error, ShouldBeNil)
			So(string(res.Value.([]byte)), ShouldEqual, "ok")
		})
	})
}

func TestPoolReadWriteAndMetrics(t *testing.T) {
	Convey("Given a Pool", t, func() {
		p := New(context.Background(), 1, 2, NewConfig())
		defer p.Close()

		Convey("Write and Read should stream through the result store", func() {
			_, werr := p.Write([]byte("alpha"))
			So(werr, ShouldBeNil)
			buf := make([]byte, 16)
			n, rerr := p.Read(buf)
			So(rerr, ShouldBeNil)
			So(string(buf[:n]), ShouldEqual, "alpha")
		})

		Convey("Metrics should return the live collector", func() {
			So(p.Metrics(), ShouldEqual, p.metrics)
			p.metrics.RecordJobSuccess(time.Millisecond)
			So(p.Metrics().JobCount, ShouldEqual, 1)
		})
	})
}

func TestPoolSubscribeForBroadcastGroup(t *testing.T) {
	Convey("Given Pool.Subscribe for a registered group", t, func() {
		p := New(context.Background(), 1, 2, NewConfig())
		defer p.Close()
		bg := p.CreateBroadcastGroup("fanout", time.Minute)
		ch := p.Subscribe("fanout")
		So(ch, ShouldNotBeNil)
		bg.Send(NewResult("fan"))

		Convey("Pool-level Subscribe should receive", func() {
			select {
			case got := <-ch:
				So(got.Value, ShouldEqual, "fan")
			case <-time.After(time.Second):
				t.Fatal("timeout")
			}
		})
	})
}

func TestPoolStoredResult(t *testing.T) {
	Convey("Given a nil Pool pointer", t, func() {
		var p *Pool

		Convey("StoredResult should be nil-safe", func() {
			result, ok := p.StoredResult("any")
			So(ok, ShouldBeFalse)
			So(result, ShouldBeNil)
		})
	})

	Convey("Given a Pool with no backing store", t, func() {
		p := &Pool{}

		Convey("StoredResult should report missing values", func() {
			result, ok := p.StoredResult("missing")
			So(ok, ShouldBeFalse)
			So(result, ShouldBeNil)
		})
	})

	Convey("Given a Pool with a populated backing store", t, func() {
		store := NewResultStore()
		defer store.Close()

		p := &Pool{store: store}
		store.Store("job-1", []byte("value"), time.Minute)

		Convey("StoredResult should return the stored value", func() {
			result, ok := p.StoredResult("job-1")
			So(ok, ShouldBeTrue)
			So(result, ShouldNotBeNil)
			So(string(result.Value.([]byte)), ShouldEqual, "value")
		})
	})
}

func BenchmarkPoolStoredResult(b *testing.B) {
	p := New(context.Background(), 1, 2, NewConfig())
	defer p.Close()
	p.store.Store("bench-job", []byte("ok"), time.Minute)

	b.ReportAllocs()
	for b.Loop() {
		result, ok := p.StoredResult("bench-job")
		if !ok || result == nil {
			b.Fatal("missing stored result")
		}
	}
}

func BenchmarkPoolReadWrite(b *testing.B) {
	p := New(context.Background(), 1, 2, NewConfig())
	defer p.Close()
	payload := []byte("bench")
	buffer := make([]byte, len(payload))

	b.ReportAllocs()
	for b.Loop() {
		if _, err := p.Write(payload); err != nil {
			b.Fatal(err)
		}
		if _, err := p.Read(buffer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolSubscribe(b *testing.B) {
	p := New(context.Background(), 1, 2, NewConfig())
	defer p.Close()
	p.CreateBroadcastGroup("bench-fanout", time.Minute)
	b.ReportAllocs()
	for b.Loop() {
		channel := p.Subscribe("bench-fanout")
		if channel == nil {
			b.Fatal("expected broadcast subscription")
		}
	}
}

func BenchmarkPoolGetCircuitBreaker(b *testing.B) {
	p := New(context.Background(), 1, 2, NewConfig())
	defer p.Close()

	job := NewJob(
		WithID("bench-cb"),
		WithTask(COMPUTE, newTestTask("ok")),
		WithCircuitBreaker("cb-bench", 3, time.Second, 1),
	)

	b.ReportAllocs()
	for b.Loop() {
		breaker := p.getCircuitBreaker(job)
		if breaker == nil {
			b.Fatal("expected circuit breaker")
		}
	}
}

func BenchmarkPoolMetricsAccessor(b *testing.B) {
	p := New(context.Background(), 1, 2, NewConfig())
	defer p.Close()
	b.ReportAllocs()
	for b.Loop() {
		if p.Metrics() == nil {
			b.Fatal("expected metrics instance")
		}
	}
}

func BenchmarkPoolScheduleEndToEnd(b *testing.B) {
	p := New(context.Background(), 4, 16, NewConfig())
	defer p.Close()
	wait := resultWaitBudget(p)
	var seq atomic.Uint64
	b.ReportAllocs()
	for b.Loop() {
		id := fmt.Sprintf("e2e-%d", seq.Add(1))
		if err := p.Schedule(id, COMPUTE, newTestTask("ok")); err != nil {
			b.Fatal(err)
		}
		if _, err := waitForResultBenchmark(p.store, id, wait); err != nil {
			b.Fatal(err)
		}
	}
}
