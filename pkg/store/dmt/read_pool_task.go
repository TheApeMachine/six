package dmt

import (
	"context"
	"io"
	"sync"
)

/*
readPoolTask implements pool.Task for legacy call sites that still submit a
function: the worker drains Read via io.Copy, so work runs on first Read.
When loop is false, fn runs once; when true, fn blocks until it returns
(long-lived background loops).
*/
type readPoolTask struct {
	ctx  context.Context
	fn   func(ctx context.Context) (any, error)
	loop bool
	mu   sync.Mutex
	done bool
}

/*
Read runs fn (blocking for loop tasks), then signals EOF.
*/
func (task *readPoolTask) Read(p []byte) (n int, err error) {
	task.mu.Lock()
	defer task.mu.Unlock()

	if task.done {
		return 0, io.EOF
	}

	if !task.loop {
		_, fnErr := task.fn(task.ctx)
		task.done = true

		if fnErr != nil {
			return 0, fnErr
		}

		return 0, io.EOF
	}

	_, fnErr := task.fn(task.ctx)
	task.done = true

	if fnErr != nil {
		return 0, fnErr
	}

	return 0, io.EOF
}

/*
Write implements io.Writer; these tasks do not consume job input bytes.
*/
func (task *readPoolTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

/*
Close implements io.Closer.
*/
func (task *readPoolTask) Close() error {
	return nil
}
