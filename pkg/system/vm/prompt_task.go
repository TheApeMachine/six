package vm

import (
	"bytes"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/compute"
)

/*
PromptTask runs one prompt/answer round-trip through a transport pipeline.
The worker pool drains Read, so the full execution stays pool-owned.
*/
type PromptTask struct {
	source    *bytes.Buffer
	backend   *compute.Backend
	route     io.Closer
	sink      *bytes.Buffer
	processed bool
	closed    bool
}

/*
NewPromptTask builds a pipeline of prompt source -> compute backend -> answer sink.
*/
func NewPromptTask(prompt []byte, backend *compute.Backend, route io.Closer) *PromptTask {
	return &PromptTask{
		source:  bytes.NewBuffer(prompt),
		backend: backend,
		route:   route,
		sink:    bytes.NewBuffer(nil),
	}
}

/*
Read drains the pipeline output.
*/
func (task *PromptTask) Read(p []byte) (n int, err error) {
	if !task.processed {
		if _, err = io.Copy(task.backend, task.source); err != nil {
			return 0, err
		}

		if err = task.backend.Close(); err != nil {
			return 0, err
		}
		task.closed = true

		if _, err = io.Copy(task.sink, task.backend); err != nil && err != io.EOF {
			return 0, err
		}

		if task.route != nil {
			if err = task.route.Close(); err != nil {
				return 0, err
			}

			task.route = nil
		}

		task.processed = true
	}

	return task.sink.Read(p)
}

/*
Write accepts the worker's result write-back without changing the round-trip.
*/
func (task *PromptTask) Write(p []byte) (n int, err error) {
	return task.sink.Write(p)
}

/*
Close tears down the pipeline components.
*/
func (task *PromptTask) Close() error {
	var closeErr error

	if task.backend != nil && !task.closed {
		if err := task.backend.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}

		task.closed = true
	}

	if task.route != nil {
		if err := task.route.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}

		task.route = nil
	}

	return closeErr
}
