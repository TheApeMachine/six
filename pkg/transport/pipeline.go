package transport

import (
	"context"
	"errors"
	"io"

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
	sink    io.Writer
	readers io.Reader
	staged  *primitive.Value
}

/*
NewPipeline allocates a pipeline that feeds data into the given sink
on Write, while Read will perform a "fold" operation on the data.
*/
func NewPipeline(
	ctx context.Context,
	sink io.Writer,
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
Read produces one folded Value frame per call. The fold semantics are:

  - Pull exactly one wire frame (core.Cfg.Value.Bytes) off the upstream
    multi-reader. Anything less is a partial frame and surfaces as an
    error rather than a silent half-Value.
  - If a previously read Value is staged, copy that staged Value into
    the freshly read one. Value.Write places the staged Value's
    Signals/Context/Gradient/Properties block into the new Value's
    asset region — that is the actual fold. The staged Value is then
    freed and replaced by the new one so the next Read folds the new
    one into whatever Value comes next.
  - Emit the (possibly folded) frame into the caller's buffer p. p
    must be large enough for one full frame; smaller buffers cannot
    hold a Value and surface as ErrShortBuffer up front rather than
    failing partway through the fold.

The previous implementation looped over every available reader inside
a single Read call and tried to write multiple frames into the same
caller buffer with a position cursor that was never reset, which made
the second iteration always trip on `value.Read(p[out:])` returning
ErrShortBuffer. One frame per call is the contract gossip.Conn and
the orchestrator already expect; honoring it removes the buffer-
overflow path entirely.
*/
func (pipeline *Pipeline) Read(p []byte) (n int, err error) {
	errnie.Trace("transport.Pipeline.Read")

	if len(p) < core.Cfg.Value.Bytes {
		return 0, errors.Join(
			io.ErrShortBuffer,
			errors.New("transport.Pipeline.Read: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	select {
	case <-pipeline.ctx.Done():
		return 0, pipeline.ctx.Err()
	default:
		frame := make([]byte, core.Cfg.Value.Bytes)

		if n, err = io.ReadFull(pipeline.readers, frame); err != nil {
			if errors.Is(err, io.EOF) && n == 0 {
				return 0, io.EOF
			}

			if errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, io.EOF
			}

			return 0, errnie.Error(err)
		}

		value := primitive.AllocValue()

		if err = value.LoadFullFrame(frame); err != nil {
			primitive.FreeValue(value)
			return 0, errnie.Error(err)
		}

		if pipeline.staged != nil {
			// 2. Community fields should leverage their gossip.Conn to make the values
			// within that community "fold" through each other, which means you write
			// one value to another (io.Copy(value1, value2)), which write the signals,
			// context, gradient, and properties regions of one value to another, which
			// allows a value to react to another value's state.
			if _, err = io.Copy(value, pipeline.staged); err != nil && !errors.Is(err, io.EOF) {
				primitive.FreeValue(value)
				return 0, errnie.Error(err)
			}
		}

		primitive.FreeValue(pipeline.staged)
		pipeline.staged = value

		if n, err = value.Read(p); err != nil && !errors.Is(err, io.EOF) {
			return 0, errnie.Error(err)
		}

		return n, nil
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
