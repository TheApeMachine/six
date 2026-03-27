package transport

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type Stream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	regions  []*primitive.Region
	readIdx  atomic.Uint32
	writeIdx atomic.Uint32
}

type streamOpts func(*Stream)

func NewStream(opts ...streamOpts) *Stream {
	stream := &Stream{}

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

	return stream
}

/*
Read draws a Value from one of the Regions in a rotating round-robin fashion.
This simulates the drawing from the mixed color pool.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	if len(stream.regions) == 0 {
		return 0, io.EOF
	}

	// Rotate read indexing to organically mix out
	idx := stream.readIdx.Add(1) % uint32(len(stream.regions))
	region := stream.regions[idx]

	return region.Read(p)
}

/*
Write pushes a Value into one of the Regions in a rotating round-robin fashion.
This simulates pouring the color pool into a mixture.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if len(stream.regions) == 0 {
		return 0, io.EOF
	}

	// Rotate write indexing to organically mix in
	idx := stream.writeIdx.Add(1) % uint32(len(stream.regions))
	region := stream.regions[idx]

	return region.Write(p)
}

func (stream *Stream) Close() error {
	stream.cancel()
	return nil
}

func WithContext(ctx context.Context) streamOpts {
	return func(stream *Stream) {
		stream.ctx, stream.cancel = context.WithCancel(ctx)
	}
}

func WithRegions(regions []*primitive.Region) streamOpts {
	return func(stream *Stream) {
		stream.regions = regions
	}
}
