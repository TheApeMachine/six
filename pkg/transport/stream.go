package transport

import (
	"context"
	"io"
	"math/rand/v2"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
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

type regionSlot struct {
	side int
	idx  int
}

func (stream *Stream) readSlots() []regionSlot {
	var slots []regionSlot
	for i := range stream.regions[0] {
		slots = append(slots, regionSlot{0, i})
	}
	for i := range stream.regions[1] {
		slots = append(slots, regionSlot{1, i})
	}
	return slots
}

func (stream *Stream) shuffleRegions() {
	r1 := rand.Uint32()
	r2 := rand.Uint32()
	if len(stream.regions[0]) > 0 {
		r := stream.regions[0]
		stream.regions[0] = append(r[1:], r[r1%uint32(len(r))])
	}
	if len(stream.regions[1]) > 0 {
		r := stream.regions[1]
		stream.regions[1] = append(r[1:], r[r2%uint32(len(r))])
	}
}

/*
Read pulls a Value frame from regions in round-robin order: it starts at
readIdx modulo slot count and tries each Region.Read until one returns data
or every slot returns EOF / short read.
*/
func (stream *Stream) Read(p []byte) (n int, err error) {
	if len(stream.regions) < 2 {
		return 0, io.EOF
	}

	slots := stream.readSlots()
	if len(slots) == 0 {
		return 0, io.EOF
	}

	stream.shuffleRegions()

	frame := stream.left[:primitive.ByteSize]
	start := int((stream.readIdx.Add(1) - 1) % uint32(len(slots)))
	for j := range slots {
		s := slots[(start+j)%len(slots)]
		reg := stream.regions[s.side][s.idx]
		rn, rerr := reg.Read(frame)
		if rerr != nil && rerr != io.EOF {
			return 0, rerr
		}
		if rn > 0 {
			return copy(p, frame[:rn]), nil
		}
	}
	return 0, io.EOF
}

/*
Write tokenizes the payload into a Value frame and enqueues a copy into every
Region (both sides), matching the previous “pour into the mixture” semantics and
keeping Region.Write’s ByteSize alignment requirement.
*/
func (stream *Stream) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(stream.regions) < 2 {
		return 0, io.EOF
	}

	stream.shuffleRegions()

	v := primitive.NewValue()
	for i, b := range p {
		_ = v.SetTokenID(i, primitive.Tokenize(b, uint64(i)))
	}
	if core.Cfg.FW >= 0 && core.Cfg.FW < primitive.Words {
		v[core.Cfg.FW] = 0
	}
	frame := make([]byte, primitive.ByteSize)
	if err := primitive.ValueToBytes(v, frame); err != nil {
		return 0, err
	}

	stream.writeIdx.Add(1)
	for side := 0; side < 2; side++ {
		for _, reg := range stream.regions[side] {
			if _, werr := reg.Write(frame); werr != nil {
				return 0, werr
			}
		}
	}
	return len(p), nil
}

func (stream *Stream) Close() error {
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

func WithBackend(backend kernel.Substrate) streamOpts {
	return func(stream *Stream) {
		stream.backend = backend
	}
}
