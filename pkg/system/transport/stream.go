package transport

import (
	"io"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/errnie"
)

const defaultBufferSize = 64 * 1024

/*
Stream is an async buffered pipe backed by a ring buffer. Writes complete
immediately when the buffer has space; reads block only when empty.
Close signals EOF via the pipe writer. When the reader observes EOF it
resets the ring buffer so the same pipe pair is reusable across payload
boundaries. Operations are applied inline per chunk: Write into the
operation, Read the transformed result back.

The default 64 KiB buffer lets io.Copy's 32 KiB writes land without
blocking, reducing mutex round-trips for medium and large payloads.
*/
type Stream struct {
	pr         *ringbuffer.PipeReader
	pw         *ringbuffer.PipeWriter
	buffer     *ringbuffer.RingBuffer
	bufferSize int
	operations []io.ReadWriteCloser
}

type streamOption func(*Stream)

/*
NewStream creates a pipe-backed Stream. Default ring buffer is 64 KiB;
override with WithBufferSize.
*/
func NewStream(opts ...streamOption) *Stream {
	stream := &Stream{
		bufferSize: defaultBufferSize,
		operations: make([]io.ReadWriteCloser, 0),
	}

	for _, opt := range opts {
		opt(stream)
	}

	ring := ringbuffer.New(stream.bufferSize)
	pr, pw := ring.Pipe()

	stream.buffer = ring
	stream.pr = pr
	stream.pw = pw

	return stream
}

/*
Read implements io.Reader. Each chunk from the pipe passes through
every registered operation (Write in, Read back) before returning.
On EOF the ring buffer resets for reuse.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	n, err = stream.pr.Read(p)

	if err != nil && err != io.EOF {
		return n, err
	}

	if n == 0 {
		if err == io.EOF {
			stream.buffer.Reset()
		}

		return 0, io.EOF
	}

	readErr := err

	if len(stream.operations) > 0 {
		for _, operation := range stream.operations {
			if _, err = operation.Write(p[:n]); err != nil {
				return 0, errnie.Error(err)
			}

			if n, err = operation.Read(p); err != nil {
				return n, errnie.Error(err)
			}
		}
	}

	if readErr == io.EOF {
		stream.buffer.Reset()
	}

	return n, readErr
}

/*
Write implements io.Writer, delegating to the pipe writer.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if n, err = stream.pw.Write(p); err != nil {
		return n, errnie.Error(err)
	}

	return n, nil
}

/*
Close signals EOF to the reader by closing the pipe writer. The ring
buffer stays alive; the reader-side Reset on EOF makes it reusable.
*/
func (stream *Stream) Close() error {
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

/*
WithOperations registers inline transforms applied per Read chunk.
*/
func WithOperations(operations ...io.ReadWriteCloser) streamOption {
	return func(stream *Stream) {
		stream.operations = append(stream.operations, operations...)
	}
}
