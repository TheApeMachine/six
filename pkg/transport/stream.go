package transport

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, 1024)
	},
}

type Stream struct {
	ctx         context.Context
	cancel      context.CancelFunc
	ttlDuration time.Duration
	expiryTimer *time.Timer
	closeOnce   sync.Once
	rb          *ringbuffer.RingBuffer
	pr          *ringbuffer.PipeReader
	pw          *ringbuffer.PipeWriter
	frame       []byte
	emitter     *Emitter
}

type StreamOption func(*Stream)

func NewStream(options ...StreamOption) *Stream {
	ctx, cancel := context.WithCancel(context.Background())

	rb := ringbuffer.New(
		primitive.ByteSize * primitive.ByteSize,
	).WithCancel(ctx)

	pr, pw := rb.Pipe()

	stream := &Stream{
		ctx:     ctx,
		cancel:  cancel,
		rb:      rb,
		pr:      pr,
		pw:      pw,
		frame:   make([]byte, primitive.ByteSize),
		emitter: NewEmitter(pr, pw, nil),
	}

	for _, option := range options {
		option(stream)
	}

	if err := validate.Require(map[string]any{
		"ctx":         stream.ctx,
		"cancel":      stream.cancel,
		"ttlDuration": stream.ttlDuration,
		"expiryTimer": stream.expiryTimer,
		"rb":          stream.rb,
		"pr":          stream.pr,
		"pw":          stream.pw,
		"frame":       stream.frame,
	}); err != nil {
		errnie.Error(NewStreamError(StreamErrFail, err))
		stream.Close()
		return nil
	}

	if stream.ttlDuration > 0 {
		stream.expiryTimer = time.AfterFunc(stream.ttlDuration, func() {
			stream.Close()
		})
	}

	return stream
}

func (stream *Stream) Read(p []byte) (n int, err error) {
	select {
	case <-stream.ctx.Done():
		return 0, io.EOF
	default:
	}

	if n, err = io.ReadFull(stream.emitter, stream.frame); err != nil {
		if errors.Is(err, io.ErrClosedPipe) {
			return n, io.EOF
		}
		return n, err
	}

	stream.resetTTL()
	return copy(p, stream.frame[:n]), nil
}

func (stream *Stream) Write(p []byte) (n int, err error) {
	select {
	case <-stream.ctx.Done():
		return 0, stream.ctx.Err()
	default:
	}

	if n, err = stream.emitter.Write(p); err != nil {
		return n, err
	}

	stream.resetTTL()
	return n, nil
}

func (stream *Stream) resetTTL() {
	if stream.expiryTimer == nil {
		return
	}

	if !stream.expiryTimer.Stop() {
		select {
		case <-stream.expiryTimer.C:
		default:
		}
	}

	stream.expiryTimer.Reset(stream.ttlDuration)
}

func (stream *Stream) Close() (err error) {
	stream.closeOnce.Do(func() {
		stream.cancel()

		if stream.expiryTimer != nil {
			stream.expiryTimer.Stop()
		}

		err = stream.pw.Close()
	})

	return err
}

func StreamWithContext(ctx context.Context) StreamOption {
	return func(stream *Stream) {
		stream.ctx, stream.cancel = context.WithCancel(ctx)
	}
}

func StreamWithTTL(ttl time.Duration) StreamOption {
	return func(stream *Stream) {
		stream.ttlDuration = ttl
	}
}

type StreamErrorType string

const (
	StreamErrFail StreamErrorType = "stream failure"
)

type StreamError struct {
	Err error
	Msg string
}

func (e *StreamError) Error() string {
	return e.Msg
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

func NewStreamError(err StreamErrorType, obj any) *StreamError {
	return &StreamError{
		Msg: string(err),
		Err: errors.New(string(err)),
	}
}
