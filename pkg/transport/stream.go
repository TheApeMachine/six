package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Stream is a framed byte pipe over a ring buffer (PipeReader / PipeWriter) plus
fan-out to Region mixers. Writes must be aligned to primitive.ByteSize; each Read
returns one full frame from the pipe. Region fan-out preserves chaotic mixing;
the ring carries the same frames for a single linear io.Reader view.
*/
type Stream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	rb      *ringbuffer.RingBuffer
	pr      *ringbuffer.PipeReader
	pw      *ringbuffer.PipeWriter
	regions [2][]*primitive.Region
	frame   []byte
}

type streamOpts func(*Stream)

func NewStream(opts ...streamOpts) *Stream {
	rb := ringbuffer.New(primitive.ByteSize * 64)
	pr, pw := rb.Pipe()

	stream := &Stream{
		rb: rb,
		pr: pr,
		pw: pw,
		regions: [2][]*primitive.Region{
			make([]*primitive.Region, 0),
			make([]*primitive.Region, 0),
		},
		frame: make([]byte, primitive.ByteSize),
	}

	for _, opt := range opts {
		opt(stream)
	}

	if err := validate.Require(map[string]any{
		"ctx":     stream.ctx,
		"cancel":  stream.cancel,
		"regions": stream.regions,
	}); err != nil {
		errnie.Error(err)
		return nil
	}

	stream.rb.WithCancel(stream.ctx)
	return stream
}

/*
Read returns one Value frame from the pipe. p must hold at least primitive.ByteSize
bytes. Data is framed by io.ReadFull on the PipeReader—no region polling here.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	if len(p) < primitive.ByteSize {
		return 0, io.ErrShortBuffer
	}

	if _, err = io.ReadFull(stream.pr, stream.frame); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, io.EOF
		}

		return 0, errnie.Error(
			NewStreamError(StreamErrFail),
			"error", err,
		)
	}

	return copy(p, stream.frame), nil
}

/*
Write forwards aligned Value frames to every Region on both sides, then enqueues
the same bytes on the PipeWriter. p must be a whole multiple of primitive.ByteSize;
building frames is the caller's responsibility.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if len(p)%primitive.ByteSize != 0 {
		return 0, errnie.Error(
			NewStreamError(StreamErrAlign),
			"length", len(p),
			"frameSize", primitive.ByteSize,
		)
	}

	if len(stream.regions[0])+len(stream.regions[1]) == 0 {
		return 0, io.EOF
	}

	for side := 0; side < 2; side++ {
		for _, reg := range stream.regions[side] {
			if _, werr := reg.Write(p); werr != nil {
				return 0, werr
			}
		}
	}

	if _, err = stream.pw.Write(p); err != nil {
		return 0, errnie.Error(
			NewStreamError(StreamErrFail),
			"error", err,
		)
	}

	return len(p), nil
}

/*
CloseWrite shuts down only the pipe writer. Readers of the stream observe EOF once
the buffer drains, while region writes are unchanged. Used after bulk hydration so
a concurrent drain goroutine can exit without tearing down the full stream.
*/
func (stream *Stream) CloseWrite() error {
	if stream.pw == nil {
		return nil
	}
	return stream.pw.Close()
}

func (stream *Stream) Close() error {
	if stream.pw != nil {
		_ = stream.pw.Close()
	}
	if stream.cancel != nil {
		stream.cancel()
	}
	return nil
}

func WithContext(ctx context.Context) streamOpts {
	return func(stream *Stream) {
		stream.ctx, stream.cancel = context.WithCancel(ctx)
	}
}

func WithRegions(regions []*primitive.Region) streamOpts {
	return func(stream *Stream) {
		regionSplit := len(regions) / 2
		stream.regions[0] = regions[:regionSplit]
		stream.regions[1] = regions[regionSplit:]
	}
}

type StreamErrorType string

const (
	StreamErrFail      StreamErrorType = "fail"
	StreamErrAlign     StreamErrorType = "align"
	StreamErrNoRegions StreamErrorType = "no_regions"
)

type StreamError struct {
	Err error
	Msg string
}

func NewStreamError(err StreamErrorType) *StreamError {
	return &StreamError{Err: errors.New(string(err)), Msg: string(err)}
}

func (err *StreamError) Error() string {
	return fmt.Sprintf("%s: %s", err.Err, err.Msg)
}
