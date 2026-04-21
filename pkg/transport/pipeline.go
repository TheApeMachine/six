package transport

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Pipeline manages a chain of io.ReadWriter components that
are being streamed into a single sink on Write, while Read
will perform a "fold" operation on the data. A fold is essentially
an io.Copy over two objects (Values) that are designed to interact
when one is written to the other. It uses an io.MultiReader, which
is fully consumed by the Read operation, preventing any duplicate
reads from the same source. To add more readers (when new Values are
emitted for example), use the Update method.
*/
type Pipeline struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	sink    io.ReadWriter
	readers io.Reader
	staged  *primitive.Value
}

/*
NewPipeline allocates a pipeline that feeds data into the given sink
on Write, while Read will perform a "fold" operation on the data.
*/
func NewPipeline(
	ctx context.Context,
	sink io.ReadWriter,
	rwcs ...io.ReadWriter,
) *Pipeline {
	ctx, cancel := context.WithCancel(ctx)

	readers := make([]io.Reader, 0, len(rwcs))
	for _, rwc := range rwcs {
		readers = append(readers, rwc)
	}

	return &Pipeline{
		ctx:     ctx,
		cancel:  cancel,
		sink:    sink,
		readers: io.MultiReader(readers...),
	}
}

/*
Update adds more readers to the pipeline.
*/
func (pipeline *Pipeline) Update(rwcs ...io.Reader) {
	errnie.Trace("transport.Pipeline.Update")

	pipeline.readers = io.MultiReader(
		pipeline.readers, io.MultiReader(rwcs...),
	)
}

/*
Read reads data from the pipeline.
*/
func (pipeline *Pipeline) Read(p []byte) (n int, err error) {
	errnie.Trace("transport.Pipeline.Read")

	select {
	case <-pipeline.ctx.Done():
		return 0, pipeline.ctx.Err()
	default:
		var (
			current = bytes.NewBuffer([]byte{})
			nn      int64
			out     int
		)

		for {
			// Read from the current reader in chunks of core.Cfg.Value.Bytes
			if nn, err = ringbuffer.New(
				core.Cfg.Value.Bytes,
			).Copy(
				current, pipeline.readers,
			); err != nil && err != io.EOF {
				return 0, errnie.Error(err)
			}

			if nn == 0 {
				break
			}

			value := primitive.AllocValue()
			value.LoadFullFrame(current.Bytes())

			if pipeline.staged != nil {
				if nn, err = ringbuffer.New(
					core.Cfg.Value.Bytes,
				).Copy(
					value, pipeline.staged,
				); err != nil && err != io.EOF {
					return 0, errnie.Error(err)
				}
			}

			primitive.FreeValue(pipeline.staged)
			pipeline.staged = value

			if out, err = value.Read(p[out:]); err != nil && err != io.EOF {
				return 0, errnie.Error(err)
			}

			n += out
			current.Reset()
		}

		return n, io.EOF
	}
}

/*
Write feeds the data into the sink.
*/
func (pipeline *Pipeline) Write(p []byte) (n int, err error) {
	errnie.Trace("transport.Pipeline.Write")

	select {
	case <-pipeline.ctx.Done():
		return 0, pipeline.ctx.Err()
	default:
		return pipeline.sink.Write(p)
	}
}

/*
Close closes the pipeline.
*/
func (pipeline *Pipeline) Close() (err error) {
	if closer, ok := pipeline.readers.(io.Closer); ok {
		if err = closer.Close(); err != nil {
			err = errors.Join(err, errnie.Error(err))
		}
	}

	return err
}
