package transport

import (
	"context"
	"fmt"
	"io"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
FrameEmit receives one reassembled frame. The slice is valid only until the
function returns; callers must copy if they retain data beyond that.
*/
type Publishable interface {
	Publish(value *primitive.Value, label string) error
}

/*
PublishedValueDrainer is a FramedBytePipe that can push whole *primitive.Value
records straight to Publishable sinks without serializing through Value.Bytes
and ValueFromWireFrame (see vm.Tokenizer).

When frameTee is non-nil (Pipeline egress), each value is still serialized once
for downstream readers that consume wire frames.
*/
type PublishedValueDrainer interface {
	DrainPublishedValues(
		ctx context.Context,
		label string,
		publishers []Publishable,
		frameTee io.Writer,
	) error
}

/*
Stream is an io.Writer sink for io.Copy: incoming chunks may split frames at
any boundary, and Stream buffers until it can deliver frameSize contiguous
bytes to emit for each full frame.

Use it between a framed Reader (for example vm.Tokenizer) and application
code that consumes whole fixed-width wire records without coupling those
consumers to io.Copy's internal buffering.
*/
type Stream struct {
	frameSize  int
	spare      []byte
	publishers []Publishable
	/*
	   frameTee receives each full reassembled frame before publishers run.
	   Optional; used by Pipeline when egress is enabled. Writes must not
	   retain the slice after returning.
	*/
	frameTee io.Writer
}

/*
NewStream constructs a stream that invokes emit once per complete frame of
frameSize bytes.

frameSize must be positive and emit must be non-nil.
*/
func NewStream(frameSize int, publishers ...Publishable) (*Stream, error) {
	if frameSize <= 0 {
		return nil, fmt.Errorf("transport.NewStream: frameSize must be positive")
	}

	if len(publishers) == 0 {
		return nil, fmt.Errorf("transport.NewStream: need at least one Publishable")
	}

	return &Stream{
		frameSize: frameSize,
		// Enough cap for one partial frame plus a large io.Copy chunk without
		// append reallocations on the hot path.
		spare:      make([]byte, 0, frameSize+32*1024),
		publishers: publishers,
	}, nil
}

/*
SetFrameTee sets the optional writer that receives every full frame before
Publish. Call before the first Write (Pipeline does this during construction).
*/
func (stream *Stream) SetFrameTee(tee io.Writer) {
	if stream == nil {
		return
	}

	stream.frameTee = tee
}

/*
Write implements io.Writer. It returns only after every full frame produced
from the prefix of (spare ‖ p) has been passed to emit without error.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if stream == nil {
		return 0, fmt.Errorf("transport.Stream.Write: nil Stream")
	}

	if len(p) == 0 {
		return 0, nil
	}

	stream.spare = append(stream.spare, p...)

	for len(stream.spare) >= stream.frameSize {
		frame := stream.spare[:stream.frameSize]
		stream.spare = stream.spare[stream.frameSize:]

		if stream.frameTee != nil {
			if _, err := stream.frameTee.Write(frame); err != nil {
				return 0, err
			}
		}

		for _, publisher := range stream.publishers {
			value, err := primitive.ValueFromWireFrame(frame)

			if err != nil {
				return 0, err
			}

			pubErr := publisher.Publish(value, "")
			_ = value.Close()

			if pubErr != nil {
				return 0, pubErr
			}
		}
	}

	return len(p), nil
}

/*
Close returns an error if the buffer still holds a partial frame after the
byte source has ended.
*/
func (stream *Stream) Close() error {
	if stream == nil {
		return nil
	}

	if len(stream.spare) == 0 {
		return nil
	}

	return fmt.Errorf(
		"transport.Stream.Close: truncated frame (%d bytes, need %d)",
		len(stream.spare),
		stream.frameSize,
	)
}

var (
	_ io.Writer      = (*Stream)(nil)
	_ io.WriteCloser = (*Stream)(nil)
)
