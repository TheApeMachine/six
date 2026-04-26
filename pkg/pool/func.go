package pool

import (
	"sync"
)

type (
	/*
		PoolWithFunc runs typed inputs over a fixed worker set.
		It mirrors Pool while avoiding per-call closure allocation when the
		work function is stable and only the payload changes.
	*/
	PoolWithFunc[T any] struct {
		jobs chan T
		done chan struct{}
		once sync.Once
		wait sync.WaitGroup
		task func(T)
	}
)

/*
NewPoolWithFunc returns a typed bounded worker pool.
*/
func NewPoolWithFunc[T any](size uint64, task func(T)) *PoolWithFunc[T] {
	if size == 0 {
		size = 1
	}

	pool := &PoolWithFunc[T]{
		jobs: make(chan T, size),
		done: make(chan struct{}),
		task: task,
	}

	pool.wait.Add(int(size))

	for worker := uint64(0); worker < size; worker++ {
		go pool.loop()
	}

	return pool
}

/*
Invoke sends one value to the shared task function.
*/
func (pool *PoolWithFunc[T]) Invoke(value T) {
	if pool == nil || pool.task == nil {
		return
	}

	select {
	case <-pool.done:
	case pool.jobs <- value:
	}
}

func (pool *PoolWithFunc[T]) loop() {
	defer pool.wait.Done()

	for {
		select {
		case <-pool.done:
			return
		case value := <-pool.jobs:
			pool.task(value)
		}
	}
}

/*
Close stops workers after their current task.
*/
func (pool *PoolWithFunc[T]) Close() error {
	if pool == nil {
		return nil
	}

	pool.once.Do(func() {
		close(pool.done)
	})

	pool.wait.Wait()

	return nil
}
