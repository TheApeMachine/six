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

Values are paired on the way through: the Stream buffers one frame and, when the
next frame arrives, writes the new frame to the buffered Value (triggering the
ALU via Value.Write). The executed result is fanned out to Regions, and the new
frame becomes the next buffer. This is the core mixing step that connects
firmware execution to the population.
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

	// pending holds one Value frame waiting for a partner. When the next frame
	// arrives, pending.Write(next) fires the ALU, the result is fanned out,
	// and the arriving frame becomes the new pending.
	pending *primitive.Value
}

type streamOpts func(*Stream)

func NewStream(opts ...streamOpts) (*Stream, error) {
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
		return nil, err
	}

	stream.rb.WithCancel(stream.ctx)
	return stream, nil
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
		// Immediate EOF (empty pipe) is io.EOF; short read then EOF is ErrUnexpectedEOF.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
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
Write enqueues aligned Value frames through the pairing pipeline: each incoming
frame is written to the previously buffered Value (triggering the ALU), the
executed result fans out to the pipe and Regions, and the incoming frame becomes
the new buffer. The first frame in a sequence is simply buffered.

p must be a whole multiple of primitive.ByteSize.
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
		return 0, NewStreamError(StreamErrNoRegions)
	}

	buf := make([]byte, primitive.ByteSize)

	for offset := 0; offset+primitive.ByteSize <= len(p); offset += primitive.ByteSize {
		chunk := p[offset : offset+primitive.ByteSize]

		if stream.pending == nil {
			// First frame: just buffer it.
			stream.pending = new(primitive.Value)
			if err := stream.pending.ApplyWireFrame(chunk); err != nil {
				return offset, err
			}
			continue
		}

		// We have a pending Value and a new frame. Execute: write the new
		// frame to the pending Value, which triggers the ALU if pending
		// carries a program.
		if _, werr := stream.pending.Write(chunk); werr != nil {
			errnie.Error(werr, "op", "stream.pair")
		}

		// Serialize the executed pending and fan it out.
		if err := primitive.ValueToBytes(stream.pending, buf); err != nil {
			return offset, err
		}
		if err := stream.fanOut(buf); err != nil {
			return offset, err
		}

		// The arriving frame becomes the new pending (unmodified).
		stream.pending.ApplyWireFrame(chunk)
	}

	return len(p), nil
}

// flushPending serializes and fans out the last buffered Value that has no
// partner yet. Called on CloseWrite / Close so the trailing frame is not lost.
func (stream *Stream) flushPending() error {
	if stream.pending == nil {
		return nil
	}
	buf := make([]byte, primitive.ByteSize)
	if err := primitive.ValueToBytes(stream.pending, buf); err != nil {
		return err
	}
	stream.pending = nil
	return stream.fanOut(buf)
}

// fanOut pushes one ByteSize frame to the pipe and every Region.
func (stream *Stream) fanOut(frame []byte) error {
	if _, err := stream.pw.Write(frame); err != nil {
		return errnie.Error(
			NewStreamError(StreamErrFail),
			"error", err,
		)
	}

	for side := 0; side < 2; side++ {
		for _, reg := range stream.regions[side] {
			if _, werr := reg.Write(frame); werr != nil {
				return werr
			}
		}
	}
	return nil
}

/*
CloseWrite flushes any pending Value and shuts down the pipe writer. Readers
of the stream observe EOF once the buffer drains.
*/
func (stream *Stream) CloseWrite() error {
	if err := stream.flushPending(); err != nil {
		return err
	}
	if stream.pw == nil {
		return nil
	}
	return stream.pw.Close()
}

func (stream *Stream) Close() error {
	_ = stream.flushPending()
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
		if len(regions) == 0 {
			stream.regions[0] = nil
			stream.regions[1] = nil
			return
		}
		regionSplit := (len(regions) + 1) / 2
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
