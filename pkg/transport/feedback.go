package transport

import (
	"context"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Feedback is a mechanism that offers a throughput path for data, while
also copying it to a backward writer.
*/
type Feedback struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	rb       *ringbuffer.RingBuffer
	backward io.Writer
	tee      io.Reader
}

/*
NewFeedback creates a new Feedback instance that manages bidirectional data flow.
*/
func NewFeedback(ctx context.Context, backward io.Writer) *Feedback {
	ctx, cancel := context.WithCancel(ctx)

	rb := ringbuffer.New(core.Cfg.Value.Bytes * 64)

	return &Feedback{
		ctx:      ctx,
		cancel:   cancel,
		rb:       rb,
		backward: backward,
		tee:      io.TeeReader(rb, backward),
	}
}

/*
Read implements io.Reader, and reads data from the TeeReader, that
wraps the ring buffer and the backward writer. This means that a
Read will both act as a straight throughput path, as well as a
copy that is sent to the backward writer.
*/
func (feedback *Feedback) Read(p []byte) (n int, err error) {
	errnie.Trace("transport.Feedback.Read")

	select {
	case <-feedback.ctx.Done():
		return 0, feedback.ctx.Err()
	default:
		return feedback.tee.Read(p)
	}
}

/*
Write implements io.Writer, and writes data to the ring buffer,
which acts as a pipe completing the throughput part of the
feedback loop.
*/
func (feedback *Feedback) Write(p []byte) (n int, err error) {
	errnie.Trace("transport.Feedback.Write")

	select {
	case <-feedback.ctx.Done():
		return 0, feedback.ctx.Err()
	default:
		if n, err = feedback.rb.Write(p); err != nil {
			return n, errnie.Error(err)
		}

		return n, nil
	}
}

/*
Close cancels the Feedback's context.
*/
func (feedback *Feedback) Close() (err error) {
	if feedback == nil {
		return nil
	}

	feedback.cancel()
	feedback.rb.CloseWriter()

	return err
}

/*
Error returns the most recent error that occurred during reading or writing.
*/
func (feedback *Feedback) Error() error {
	return feedback.err
}
