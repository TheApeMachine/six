package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

// maxPoolBufCap limits pooled slice backing arrays so a single Read cannot return
// a huge buffer to the pool after unbounded appends.
const maxPoolBufCap = 4096

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, 1024)
	},
}

func putBufToPool(b []byte) {
	if cap(b) > maxPoolBufCap {
		bufPool.Put(make([]byte, 0, 1024))
		return
	}
	bufPool.Put(b[:0:cap(b)])
}

type Stream struct {
	ctx         context.Context
	cancel      context.CancelFunc
	ttlDuration time.Duration
	expiryTimer *time.Timer
	closeOnce   sync.Once
	regions     int
	rb          []*ringbuffer.RingBuffer
	pr          []*ringbuffer.PipeReader
	pw          []*ringbuffer.PipeWriter
	frame       [][]byte
	emitter     []*Emitter
	ptr         int
}

type StreamOption func(*Stream)

func (stream *Stream) configureEmitters(n int) {
	if n < 1 {
		n = 1
	}

	stream.regions = n
	stream.rb = make([]*ringbuffer.RingBuffer, 0, n)
	stream.pr = make([]*ringbuffer.PipeReader, 0, n)
	stream.pw = make([]*ringbuffer.PipeWriter, 0, n)
	stream.frame = make([][]byte, 0, n)
	stream.emitter = make([]*Emitter, 0, n)

	for range n {
		rb := ringbuffer.New(primitive.ByteSize * primitive.ByteSize)
		pr, pw := rb.Pipe()
		stream.rb = append(stream.rb, rb)
		stream.pr = append(stream.pr, pr)
		stream.pw = append(stream.pw, pw)
		stream.frame = append(stream.frame, make([]byte, primitive.ByteSize))
		stream.emitter = append(stream.emitter, NewEmitter(pr, pw, nil))
	}
}

func NewStream(options ...StreamOption) *Stream {
	ctx, cancel := context.WithCancel(context.Background())

	stream := &Stream{
		ctx:     ctx,
		cancel:  cancel,
		rb:      make([]*ringbuffer.RingBuffer, 0),
		pr:      make([]*ringbuffer.PipeReader, 0),
		pw:      make([]*ringbuffer.PipeWriter, 0),
		frame:   make([][]byte, 0),
		emitter: make([]*Emitter, 0),
	}

	for _, option := range options {
		option(stream)
	}

	// The stream must always expose at least one emitter path. This keeps
	// Write/Read balanced even when callers no longer configure "regions".
	if len(stream.emitter) == 0 {
		stream.configureEmitters(1)
	}

	// ttlDuration / expiryTimer are intentionally omitted: expiryTimer is created
	// below only when ttlDuration > 0, and a nil *time.Timer in the map would
	// fail validate.Require even when TTL is disabled.
	if err := validate.Require(map[string]any{
		"ctx":    stream.ctx,
		"cancel": stream.cancel,
		"rb":     stream.rb,
		"pr":     stream.pr,
		"pw":     stream.pw,
		"frame":  stream.frame,
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

	buf := bufPool.Get().([]byte)[:0]
	defer putBufToPool(buf)

	for i := range len(stream.emitter) {
		idx := (i + stream.ptr) % stream.regions

		if n, err = io.ReadFull(
			stream.emitter[idx],
			stream.frame[idx],
		); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return n, io.EOF
			}

			return n, err
		}

		buf = append(buf, stream.frame[idx][:n]...)
	}

	stream.resetTTL()
	return copy(p, buf), nil
}

func (stream *Stream) Write(p []byte) (n int, err error) {
	select {
	case <-stream.ctx.Done():
		return 0, stream.ctx.Err()
	default:
	}

	for _, emitter := range stream.emitter {
		if n, err = emitter.Write(p); err != nil {
			return n, err
		}
	}

	stream.ptr++
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

		var closeErrs []error
		for _, pw := range stream.pw {
			if cerr := pw.Close(); cerr != nil {
				closeErrs = append(closeErrs, cerr)
			}
		}
		err = errors.Join(closeErrs...)
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

func StreamWithRegions(n int) StreamOption {
	return func(stream *Stream) {
		stream.configureEmitters(n)
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
	msg := string(err)
	var cause error
	if obj != nil {
		if e, ok := obj.(error); ok {
			cause = e
			msg = fmt.Sprintf("%s: %v", err, e)
		} else {
			msg = fmt.Sprintf("%s: %v", err, obj)
			cause = fmt.Errorf("%s: %v", err, obj)
		}
	}
	return &StreamError{
		Msg: msg,
		Err: cause,
	}
}
