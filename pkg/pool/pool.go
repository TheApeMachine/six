package pool

import (
	"sync"
)

type Task interface {
	Workload() func()
	Optimize()
}

/*
Pool runs function workloads over a fixed worker set.
The previous parked-goroutine stack used private runtime semaphores, which
made race verification impossible for the execution path it was meant to
protect. The bounded channel keeps backpressure explicit while staying inside
the public Go memory model.
*/
type Pool struct {
	jobs chan func()
	done chan struct{}
	once sync.Once
	wait sync.WaitGroup
}

/*
NewPool returns a bounded worker pool.
*/
func NewPool(size uint64) *Pool {
	if size == 0 {
		size = 1
	}

	pool := &Pool{
		jobs: make(chan func(), size),
		done: make(chan struct{}),
	}

	pool.wait.Add(int(size))

	for worker := uint64(0); worker < size; worker++ {
		go pool.loop()
	}

	return pool
}

/*
Submit queues a task, blocking when all workers are busy.
*/
func (pool *Pool) Submit(task func()) {
	if pool == nil || task == nil {
		return
	}

	select {
	case <-pool.done:
	case pool.jobs <- task:
	}
}

func (pool *Pool) loop() {
	defer pool.wait.Done()

	for {
		select {
		case <-pool.done:
			return
		case task := <-pool.jobs:
			if task != nil {
				task()
			}
		}
	}
}

/*
Close stops workers after their current task.
*/
func (pool *Pool) Close() error {
	if pool == nil {
		return nil
	}

	pool.once.Do(func() {
		close(pool.done)
	})

	pool.wait.Wait()

	return nil
}
