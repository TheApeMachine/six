package transport

import (
	"errors"
	"fmt"
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

const InstructionByteMask byte = 0x80 // 10000000 in binary
const defaultBufferSize = 1024

/*
Stream is an async buffered pipe backed by a ring buffer. Writes complete
immediately when the buffer has space; reads block only when empty.
Close signals EOF via the pipe writer. When the reader observes EOF it
resets the ring buffer so the same pipe pair is reusable across payload
boundaries.

In-Band Control Frame Interception:
Operations are applied inline per chunk. However, if a 1024-byte chunk is
flagged as a control frame (instruction bit set), the Stream consumes the
chunk to physically rewire its own operational pipeline, never emitting it
to the caller.
*/
type Stream struct {
	pr         *ringbuffer.PipeReader
	pw         *ringbuffer.PipeWriter
	buffer     *ringbuffer.RingBuffer
	operation  io.ReadWriteCloser
	bufferSize int
}

type streamOption func(*Stream)

/*
NewStream creates a pipe-backed Stream. Default ring buffer is 64 KiB;
override with WithBufferSize.
*/
func NewStream(opts ...streamOption) *Stream {
	ring := ringbuffer.New(defaultBufferSize)
	pr, pw := ring.Pipe()

	stream := &Stream{
		buffer: ring,
		pr:     pr,
		pw:     pw,
	}

	for _, opt := range opts {
		opt(stream)
	}

	if err := validate.Require(map[string]any{
		"buffer": stream.buffer,
		"pr":     stream.pr,
		"pw":     stream.pw,
	}); err != nil {
		return nil
	}

	return stream
}

/*
Read implements io.Reader. It processes the pipe strictly in 1024-byte
primitive.ByteSize chunks. It intercepts in-band control frames, reconfigures
itself, and applies active operations to the remaining data.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	if stream.operation != nil {
		n, err = stream.operation.Read(p)

		if err != nil {
			if err == io.EOF {
				_ = stream.operation.Close()
				stream.operation = nil

				if n > 0 {
					return n, nil
				}
			}

			return 0, err
		}

		if n > 0 {
			_ = stream.operation.Close()
			stream.operation = nil
		}

		return n, nil
	}

	if stream.buffer.Length() == 0 {
		return 0, io.EOF
	}

	n, err = stream.pr.Read(p)

	if err != nil && err != io.EOF {
		errnie.Error(err)
		return 0, err
	}

	if n == 0 {
		return 0, io.EOF
	}

	return n, err
}

/*
Write implements io.Writer, delegating to the pipe writer.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if stream.operation == nil && len(p) >= primitive.ByteSize && p[len(p)-1]&InstructionByteMask != 0 {
		stream.operation = operation.NewBitwise(operation.AND)
	}

	if stream.operation != nil {
		return stream.operation.Write(p)
	}

	if n, err = stream.pw.Write(p); errnie.Error(err) != nil {
		return
	}

	return n, nil
}

/*
Close signals EOF to the reader by closing the pipe writer. The ring
buffer stays alive; the reader-side Reset on EOF makes it reusable.
*/
func (stream *Stream) Close() error {
	if stream.operation != nil {
		stream.operation.Close()
	}

	stream.buffer.Reset()
	return stream.pw.Close()
}

/*
WithBufferSize sets the ring buffer capacity in bytes. Larger buffers
reduce mutex round-trips for big payloads at the cost of memory.
*/
func WithBufferSize(size int) streamOption {
	return func(stream *Stream) {
		stream.bufferSize = size
	}
}

type StreamErrorType string

const (
	StreamErrorTypeBufferNil   StreamErrorType = "buffer is nil"
	StreamErrorTypeBufferEmpty StreamErrorType = "buffer is empty"
	StreamErrorTypeBufferFull  StreamErrorType = "buffer is full"
	StreamErrorTypeBufferRead  StreamErrorType = "buffer read error"
	StreamErrorTypeBufferWrite StreamErrorType = "buffer write error"
)

type StreamError struct {
	Message string
	Err     error
}

func NewStreamError(err StreamErrorType) *StreamError {
	return &StreamError{Message: string(err), Err: errors.New(string(err))}
}

func (err StreamError) Error() string {
	return fmt.Sprintf("stream error: %s (%s)", err.Message, err.Err)
}
