package transport

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/pkg/errnie"
)

// ErrEmptyPipeline is returned by Pipeline.Write when the pipeline has no
// components to forward data to. Returning an error rather than silently
// discarding bytes prevents callers from believing a write succeeded when
// the data has nowhere to go.
var ErrEmptyPipeline = errors.New("transport: write to empty pipeline")

/*
Pipeline manages a chain of io.ReadWriteCloser components.

It connects components together so data flows through all components in sequence.
Each component can produce data independently.
*/
type Pipeline struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	components []io.ReadWriter
	processed  bool
}

/*
NewPipeline creates a pipeline connecting io.ReadWriteCloser components.

It connects components together so data written to the pipeline flows through
all components in sequence.

Example:

	// Simple pipeline
	p := workflow.NewPipeline(message, agent, provider)
	io.Copy(os.Stdout, p)

	// Nested pipelines
	p1 := workflow.NewPipeline(message, agent, provider)
	p2 := workflow.NewPipeline(message, agent, provider, p1)
	io.Copy(os.Stdout, p2)
*/
func NewPipeline(ctx context.Context, components ...io.ReadWriter) io.ReadWriteCloser {
	ctx, cancel := context.WithCancel(ctx)

	return &Pipeline{
		ctx:        ctx,
		cancel:     cancel,
		components: components,
	}
}

/*
Read implements the io.Reader interface.

It reads from the first component and passes data through the pipeline.
Returns EOF when no more data is available.
*/
func (pipeline *Pipeline) Read(p []byte) (n int, err error) {
	if len(pipeline.components) == 0 {
		return 0, io.EOF
	}

	select {
	case <-pipeline.ctx.Done():
		return 0, pipeline.ctx.Err()
	default:
		if !pipeline.processed {
			for i := range len(pipeline.components) - 1 {
				// Bytes copied between intermediate components are not bytes
				// delivered to p, so they must not contribute to n (io.Reader
				// requires n <= len(p)). The copy itself still has to happen
				// so the final component has data to read from.
				if _, err = io.Copy(pipeline.components[i+1], pipeline.components[i]); err != nil {
					if err == io.EOF {
						continue
					}

					return 0, errnie.Error(err)
				}
			}

			pipeline.processed = true
		}

		n, err = pipeline.components[len(pipeline.components)-1].Read(p)

		if err != nil {
			if err == io.EOF {
				pipeline.processed = false
				return n, err
			}

			return n, errnie.Error(err)
		}

		if n == 0 {
			pipeline.processed = false
			return n, io.EOF
		}

		return n, nil
	}
}

/*
Write implements the io.Writer interface.

It writes data to the first component in the pipeline.
Note that writing is optional - components can produce data independently.
*/
func (pipeline *Pipeline) Write(p []byte) (n int, err error) {
	if len(pipeline.components) == 0 {
		return 0, errnie.Error(ErrEmptyPipeline)
	}

	select {
	case <-pipeline.ctx.Done():
		return 0, pipeline.ctx.Err()
	default:
		pipeline.processed = false
		return pipeline.components[0].Write(p)
	}
}

/*
Close implements the io.Closer interface.

It closes all components in the pipeline that implement io.Closer.
*/
func (pipeline *Pipeline) Close() error {
	var firstErr error

	for _, component := range pipeline.components {
		closer, ok := component.(io.Closer)
		if !ok {
			continue
		}

		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = errnie.Error(err)
		}
	}

	return firstErr
}
