package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

const timeoutMsg = "Test timed out waiting for value retrieval"

func TestWorker(t *testing.T) {
	Convey("Given a worker", t, func() {
		Convey("It should process a job successfully", func() {
			pool := &Pool{
				ctx:     context.Background(),
				workers: make(chan chan Job, 1),
				store:   NewResultStore(),
				metrics: NewMetrics(),
			}

			worker := &Worker{
				pool: pool,
				ctx:  context.Background(),
				jobs: make(chan Job, 1),
			}

			Reset(func() {
				close(worker.jobs)
				pool.store.Close()
			})

			job := Job{
				ID:           "job_success",
				Fn:           func(ctx context.Context) (any, error) { return "result", nil },
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{},
			}

			worker.jobs <- job
			go worker.run()

			result := pool.store.Await(job.ID)
			select {
			case <-time.After(2 * time.Second):
				t.Fatal(timeoutMsg)
			case value := <-result:
				So(value.Error, ShouldBeNil)
				So(value.Value, ShouldEqual, "result")
			}
		})

		Convey("It should handle job timeout", func() {
			pool := &Pool{
				ctx:     context.Background(),
				workers: make(chan chan Job, 1),
				store:   NewResultStore(),
				metrics: NewMetrics(),
			}

			worker := &Worker{
				pool: pool,
				ctx:  context.Background(),
				jobs: make(chan Job, 1),
			}

			Reset(func() {
				close(worker.jobs)
				pool.store.Close()
			})

			job := Job{
				ID: "job_timeout",
				Fn: func(ctx context.Context) (any, error) {
					time.Sleep(200 * time.Millisecond)
					return nil, nil
				},
				StartTime:    time.Now(),
				TTL:          100 * time.Millisecond,
				Dependencies: []string{},
			}

			ctx, cancel := context.WithTimeout(pool.ctx, 100*time.Millisecond)
			defer cancel()
			worker.pool.ctx = ctx

			worker.jobs <- job
			go worker.run()

			result := pool.store.Await(job.ID)
			select {
			case <-time.After(2 * time.Second):
				t.Fatal(timeoutMsg)
			case value := <-result:
				So(value.Error, ShouldNotBeNil)
				So(value.Error.Error(), ShouldContainSubstring, "timed out")
			}
		})

		Convey("It should not process a job if dependencies are not met", func() {
			pool := &Pool{
				ctx:     context.Background(),
				workers: make(chan chan Job, 1),
				store:   NewResultStore(),
				metrics: NewMetrics(),
			}

			worker := &Worker{
				pool: pool,
				ctx:  context.Background(),
				jobs: make(chan Job, 1),
			}

			Reset(func() {
				close(worker.jobs)
				pool.store.Close()
			})

			job := Job{
				ID:           "job_dependency",
				Fn:           func(ctx context.Context) (any, error) { return "result", nil },
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{"dep1"},
			}

			worker.jobs <- job
			go worker.run()

			result := pool.store.Await(job.ID)
			select {
			case <-time.After(2 * time.Second):
				t.Fatal(timeoutMsg)
			case value := <-result:
				So(value.Error, ShouldNotBeNil)
				So(value.Error.Error(), ShouldContainSubstring, "dependency dep1 failed")
			}
		})

		Convey("It should retry a failed job using retry policy", func() {
			pool := &Pool{
				ctx:     context.Background(),
				workers: make(chan chan Job, 1),
				store:   NewResultStore(),
				metrics: NewMetrics(),
			}

			worker := &Worker{
				pool: pool,
				ctx:  context.Background(),
				jobs: make(chan Job, 1),
			}

			Reset(func() {
				close(worker.jobs)
				pool.store.Close()
			})

			attempts := 0
			job := Job{
				ID: "job_retry_success",
				Fn: func(ctx context.Context) (any, error) {
					attempts++
					if attempts < 3 {
						return nil, context.DeadlineExceeded
					}
					return "ok-after-retry", nil
				},
				RetryPolicy: &RetryPolicy{
					MaxAttempts: 3,
					Strategy:    &ExponentialBackoff{Initial: time.Millisecond},
				},
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{},
			}

			worker.jobs <- job
			go worker.run()

			result := pool.store.Await(job.ID)
			select {
			case <-time.After(2 * time.Second):
				t.Fatal(timeoutMsg)
			case value := <-result:
				So(value.Error, ShouldBeNil)
				So(value.Value, ShouldEqual, "ok-after-retry")
				So(attempts, ShouldEqual, 3)
			}
		})
	})
}

func BenchmarkWorkerProcessJobWithRetry(b *testing.B) {
	workerPool := &Pool{
		ctx:      context.Background(),
		store:    NewResultStore(),
		metrics:  NewMetrics(),
		breakers: map[string]*CircuitBreaker{},
	}
	defer workerPool.store.Close()

	worker := &Worker{
		pool: workerPool,
		ctx:  context.Background(),
		jobs: make(chan Job, 1),
	}

	b.ReportAllocs()

	for b.Loop() {
		attempts := 0
		job := Job{
			ID: "bench-retry",
			Fn: func(ctx context.Context) (any, error) {
				attempts++
				if attempts < 3 {
					return nil, errors.New("transient")
				}
				return attempts, nil
			},
			RetryPolicy: &RetryPolicy{
				MaxAttempts: 3,
			},
			StartTime: time.Now(),
		}

		value, err := worker.processJobWithTimeout(context.Background(), job)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		if value != 3 {
			b.Fatalf("unexpected value: %v", value)
		}
	}
}
