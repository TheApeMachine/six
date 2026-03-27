package transport

import (
	"context"
	"io"
	"math/rand/v2"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type Stream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	regions  [2][]*primitive.Region
	backend  kernel.Substrate
	readIdx  atomic.Uint32
	writeIdx atomic.Uint32
	left     []byte
	right    []byte
}

type streamOpts func(*Stream)

func NewStream(opts ...streamOpts) *Stream {
	stream := &Stream{
		regions: [2][]*primitive.Region{
			make([]*primitive.Region, 0),
			make([]*primitive.Region, 0),
		},
		left:  make([]byte, primitive.ByteSize),
		right: make([]byte, primitive.ByteSize),
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

	return stream
}

/*
Read draws a Value from one of the Regions in a rotating round-robin fashion.
When possible, it draws a pair of Values to fold them (in-band execution pointer),
pushing the folded task to the global hardware bus.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	if len(stream.regions) == 0 {
		return 0, io.EOF
	}

	// Generate a random number.
	rand1 := rand.Uint32()
	rand2 := rand.Uint32()

	// Rotate the regions based on the random number.
	stream.regions[0] = append(stream.regions[0][1:], stream.regions[0][rand1%uint32(len(stream.regions[0]))])
	stream.regions[1] = append(stream.regions[1][1:], stream.regions[1][rand2%uint32(len(stream.regions[1]))])

	// Read the data from the regions.
	for _, region := range stream.regions {
		for _, value := range region {
			value.Read(p)
		}
	}

	return len(p), nil
}

/*
Write pushes a Value into one of the Regions in a rotating round-robin fashion.
This simulates pouring the color pool into a mixture.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if len(stream.regions) == 0 {
		return 0, io.EOF
	}

	// Generate a random number.
	rand1 := rand.Uint32()
	rand2 := rand.Uint32()

	// Rotate the regions based on the random number.
	stream.regions[0] = append(stream.regions[0][1:], stream.regions[0][rand1%uint32(len(stream.regions[0]))])
	stream.regions[1] = append(stream.regions[1][1:], stream.regions[1][rand2%uint32(len(stream.regions[1]))])

	// Write the data to the regions.
	for _, region := range stream.regions {
		for _, value := range region {
			value.Write(p)
		}
	}

	return len(p), nil
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
		regionSplit := len(regions) / 2
		stream.regions[0] = regions[:regionSplit]
		stream.regions[1] = regions[regionSplit:]
	}
}

func WithBackend(backend kernel.Substrate) streamOpts {
	return func(stream *Stream) {
		stream.backend = backend
	}
}
