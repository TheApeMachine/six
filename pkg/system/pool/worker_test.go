package pool

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

const timeoutMsg = "Test timed out waiting for value retrieval"

func waitForWorkerResult(store *ResultStore, id string, timeout time.Duration) (*Result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, ok := store.Result(id)
		if ok {
			return result, nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil, errors.New(timeoutMsg)
}

type workerTask struct {
	reader *bytes.Reader
	err    error
	delay  time.Duration
	reads  int
}

func newWorkerTask(payload string) *workerTask {
	return &workerTask{
		reader: bytes.NewReader([]byte(payload)),
	}
}

func (task *workerTask) Read(p []byte) (n int, err error) {
	task.reads++

	if task.delay > 0 {
		time.Sleep(task.delay)
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

func (task *workerTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (task *workerTask) Close() error {
	return nil
}

type retryWorkerTask struct {
	attempts int
}

func (task *retryWorkerTask) Read(p []byte) (n int, err error) {
	task.attempts++

	if task.attempts < 3 {
		return 0, errors.New("transient")
	}

	n = copy(p, []byte("ok-after-retry"))
	return n, io.EOF
}

func (task *retryWorkerTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (task *retryWorkerTask) Close() error {
	return nil
}

func TestWorker(t *testing.T) {
	Convey("Given a worker", t, func() {
		Convey("It should process a job successfully", func() {
			pool := &Pool{
				ctx:     context.Background(),
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
				Task:         newWorkerTask("result"),
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{},
			}

			worker.jobs <- job
			go worker.run()

			value, err := waitForWorkerResult(pool.store, job.ID, 2*time.Second)
			So(err, ShouldBeNil)
			So(value.Error, ShouldBeNil)
			So(string(value.Value.([]byte)), ShouldEqual, "result")
		})

		Convey("It should handle job timeout", func() {
			pool := &Pool{
				ctx:     context.Background(),
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
				ID:           "job_timeout",
				Task:         &workerTask{delay: 200 * time.Millisecond},
				StartTime:    time.Now(),
				TTL:          100 * time.Millisecond,
				Dependencies: []string{},
			}

			ctx, cancel := context.WithTimeout(pool.ctx, 100*time.Millisecond)
			defer cancel()
			worker.pool.ctx = ctx

			worker.jobs <- job
			go worker.run()

			value, err := waitForWorkerResult(pool.store, job.ID, 2*time.Second)
			So(err, ShouldBeNil)
			So(value.Error, ShouldNotBeNil)
			So(value.Error.Error(), ShouldContainSubstring, "timed out")
		})

		Convey("It should not process a job if dependencies are not met", func() {
			pool := &Pool{
				ctx:     context.Background(),
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
				Task:         newWorkerTask("result"),
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{"dep1"},
			}

			worker.jobs <- job
			go worker.run()

			value, err := waitForWorkerResult(pool.store, job.ID, 2*time.Second)
			So(err, ShouldBeNil)
			So(value.Error, ShouldNotBeNil)
			So(value.Error.Error(), ShouldContainSubstring, "dependency dep1 failed")
		})

		Convey("It should run when dependencies already succeeded in the store", func() {
			pool := &Pool{
				ctx:     context.Background(),
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

			pool.store.Store("ready-dep", []byte("dep-bytes"), 10*time.Second)

			job := Job{
				ID:           "job_dep_ok",
				Task:         newWorkerTask("final"),
				StartTime:    time.Now(),
				TTL:          10 * time.Second,
				Dependencies: []string{"ready-dep"},
			}

			worker.jobs <- job
			go worker.run()

			value, err := waitForWorkerResult(pool.store, job.ID, 2*time.Second)
			So(err, ShouldBeNil)
			So(value.Error, ShouldBeNil)
			So(string(value.Value.([]byte)), ShouldEqual, "final")
		})

		Convey("It should retry a failed job using retry policy", func() {
			pool := &Pool{
				ctx:     context.Background(),
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
			retryTask := &retryWorkerTask{}
			job := Job{
				ID:   "job_retry_success",
				Task: retryTask,
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

			value, err := waitForWorkerResult(pool.store, job.ID, 2*time.Second)
			So(err, ShouldBeNil)
			So(value.Error, ShouldBeNil)
			So(string(value.Value.([]byte)), ShouldEqual, "ok-after-retry")
			attempts = retryTask.attempts
			So(attempts, ShouldEqual, 3)
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
		retryTask := &retryWorkerTask{}
		job := Job{
			ID:   "bench-retry",
			Task: retryTask,
			RetryPolicy: &RetryPolicy{
				MaxAttempts: 3,
			},
			StartTime: time.Now(),
		}

		value, err := worker.processJobWithTimeout(context.Background(), job)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		if string(value.([]byte)) != "ok-after-retry" {
			b.Fatalf("unexpected value: %v", value)
		}
		if retryTask.attempts != 3 {
			b.Fatalf("unexpected attempts: %d", retryTask.attempts)
		}
	}
}

func BenchmarkWorkerCheckSingleDependencySuccess(b *testing.B) {
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
	}

	workerPool.store.Store("dep-ready", []byte("ok"), time.Minute)

	b.ReportAllocs()
	for b.Loop() {
		if err := worker.checkSingleDependency("dep-ready", &RetryPolicy{
			MaxAttempts: 1,
		}); err != nil {
			b.Fatalf("unexpected dependency error: %v", err)
		}
	}
}

func BenchmarkWorkerRecordFailureAndSuccess(b *testing.B) {
	workerPool := &Pool{
		ctx:      context.Background(),
		store:    NewResultStore(),
		metrics:  NewMetrics(),
		breakers: map[string]*CircuitBreaker{},
	}
	defer workerPool.store.Close()

	workerPool.breakers["bench-circuit"] = NewCircuitBreaker(100, time.Second, 10)

	worker := &Worker{
		pool: workerPool,
		ctx:  context.Background(),
	}

	b.ReportAllocs()
	for b.Loop() {
		worker.recordFailure("bench-circuit")
		worker.recordSuccess("bench-circuit")
	}
}

func TestWorkerCheckSingleDependencySuccessAllocations(t *testing.T) {
	Convey("Given a worker and a dependency that already succeeded", t, func() {
		workerPool := &Pool{
			ctx:      context.Background(),
			store:    NewResultStore(),
			metrics:  NewMetrics(),
			breakers: map[string]*CircuitBreaker{},
		}
		defer workerPool.store.Close()

		workerPool.store.Store("dep-ready", []byte("ok"), time.Minute)

		worker := &Worker{
			pool: workerPool,
			ctx:  context.Background(),
		}

		Convey("checkSingleDependency should avoid heap allocations on success", func() {
			allocs := testing.AllocsPerRun(1000, func() {
				err := worker.checkSingleDependency("dep-ready", nil)
				if err != nil {
					t.Fatalf("unexpected dependency error: %v", err)
				}
			})

			So(allocs, ShouldEqual, 0)
		})
	})
}
