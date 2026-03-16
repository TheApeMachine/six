package pool

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestScheduleAndRetrieve(t *testing.T) {
	Convey("Given a Pool with 2 min and 4 max workers", t, func() {
		workerPool := New(context.Background(), 2, 4, NewConfig())
		defer workerPool.Close()

		Convey("When Schedule is called with a job that returns 42", func() {
			resultCh := workerPool.Schedule("job-1", func(ctx context.Context) (any, error) {
				return 42, nil
			})

			Convey("The result channel should receive value 42 with no error", func() {
				select {
				case result := <-resultCh:
					So(result.Error, ShouldBeNil)
					So(result.Value, ShouldEqual, 42)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for result")
				}
			})
		})
	})
}

func TestScheduleError(t *testing.T) {
	Convey("Given a Pool", t, func() {
		p := New(context.Background(), 2, 4, NewConfig())
		defer p.Close()

		Convey("When Schedule is called with a job that returns an error", func() {
			ch := p.Schedule("job-err", func(ctx context.Context) (any, error) {
				return nil, fmt.Errorf("intentional failure")
			})

			Convey("The result channel should receive the error", func() {
				select {
				case r := <-ch:
					So(r.Error, ShouldNotBeNil)
					So(r.Error.Error(), ShouldEqual, "intentional failure")
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for error result")
				}
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
			channels := make([]chan *Result, n)
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("concurrent-%d", i)
				channels[i] = p.Schedule(id, func(ctx context.Context) (any, error) {
					atomic.AddInt64(&completed, 1)
					return "done", nil
				})
			}

			Convey("All jobs should complete successfully", func() {
				for i, ch := range channels {
					select {
					case r := <-ch:
						So(r.Error, ShouldBeNil)
						So(r.Value, ShouldEqual, "done")
					case <-time.After(10 * time.Second):
						t.Fatalf("job %d timed out", i)
					}
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
			first := p.Schedule("repeat", func(ctx context.Context) (any, error) {
				return "first", nil
			})

			second := p.Schedule("repeat", func(ctx context.Context) (any, error) {
				return "second", nil
			})

			Convey("Each schedule should receive its own result", func() {
				select {
				case result := <-first:
					So(result.Error, ShouldBeNil)
					So(result.Value, ShouldEqual, "first")
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for first repeated result")
				}

				select {
				case result := <-second:
					So(result.Error, ShouldBeNil)
					So(result.Value, ShouldEqual, "second")
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for second repeated result")
				}
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := p.Schedule(fmt.Sprintf("bench-%d", i), func(ctx context.Context) (any, error) {
			return i, nil
		})
		<-ch
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


