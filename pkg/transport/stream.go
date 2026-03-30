package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"sync"
	"time"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

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
	pending     []int
	ptr         int
	adapters    []io.ReadWriter
}

type StreamOption func(*Stream)

func NewStream(ctx context.Context, options ...StreamOption) *Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	stream := &Stream{
		ctx:      ctx,
		cancel:   cancel,
		rb:       make([]*ringbuffer.RingBuffer, 0),
		pr:       make([]*ringbuffer.PipeReader, 0),
		pw:       make([]*ringbuffer.PipeWriter, 0),
		frame:    make([][]byte, 0),
		emitter:  make([]*Emitter, 0),
		pending:  make([]int, 0),
		adapters: make([]io.ReadWriter, 0),
	}

	for _, option := range options {
		option(stream)
	}

	if stream.regions < 1 || len(stream.emitter) == 0 {
		StreamWithRegions(1)(stream)
	}

	if err := validate.Require(map[string]any{
		"ctx":      stream.ctx,
		"cancel":   stream.cancel,
		"rb":       stream.rb,
		"pr":       stream.pr,
		"pw":       stream.pw,
		"frame":    stream.frame,
		"emitter":  stream.emitter,
		"pending":  stream.pending,
		"ptr":      stream.ptr,
		"regions":  stream.regions,
		"adapters": stream.adapters,
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

	copied := 0
	for copied+primitive.ByteSize <= len(p) {
		idx := stream.nextReadableRegion()
		if idx < 0 {
			if copied > 0 {
				stream.resetTTL()
				return copied, nil
			}
			return 0, io.EOF
		}

		hadWait := stream.emitter[idx].wait != nil
		var readN int
		if readN, err = io.ReadFull(stream.emitter[idx], stream.frame[idx]); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				if copied > 0 {
					stream.resetTTL()
					return copied, nil
				}
				return 0, io.EOF
			}
			return copied, err
		}

		if !hadWait {
			stream.pending[idx] = max(stream.pending[idx]-1, 0)
		}

		for _, adapter := range stream.adapters {
			_, _ = adapter.Write(stream.frame[idx][:readN])
		}

		copied += copy(p[copied:], stream.frame[idx][:readN])
		stream.ptr = (idx + 1) % stream.regions

		if len(p)-copied < primitive.ByteSize {
			break
		}
	}

	stream.resetTTL()
	return copied, nil
}

func (stream *Stream) Write(p []byte) (n int, err error) {
	select {
	case <-stream.ctx.Done():
		return 0, stream.ctx.Err()
	default:
	}

	if len(p)%primitive.ByteSize != 0 {
		return 0, io.ErrShortBuffer
	}

	written := 0
	for offset := 0; offset < len(p); offset += primitive.ByteSize {
		frame := p[offset : offset+primitive.ByteSize]
		idx := stream.routeRegion(frame)
		if n, err = stream.emitter[idx].Write(frame); err != nil {
			return written + n, err
		}
		stream.pending[idx]++
		written += n
	}

	stream.resetTTL()
	return written, nil
}

func (stream *Stream) nextReadableRegion() int {
	if stream == nil || stream.regions == 0 {
		return -1
	}
	for i := 0; i < stream.regions; i++ {
		idx := (stream.ptr + i) % stream.regions
		if stream.pending[idx] > 0 {
			return idx
		}
	}
	return -1
}

func (stream *Stream) routeRegion(frame []byte) int {
	if stream == nil || stream.regions <= 1 {
		return 0
	}
	affinityWord := frameAffinity(frame)
	usableAffinity := affinityWord & 0x0000FFFFFFFFFFFF
	routeBits := bits.Len(uint(stream.regions - 1))
	if routeBits <= 0 {
		return 0
	}
	shift := 48 - routeBits
	if shift < 0 {
		shift = 0
	}
	idx := int((usableAffinity >> shift) & ((1 << routeBits) - 1))
	if idx >= stream.regions {
		idx %= stream.regions
	}
	return idx
}

func frameAffinity(frame []byte) uint64 {
	start := core.Cfg.AffinityIndex * 8
	end := start + 8
	if start < 0 || end > len(frame) {
		return 0
	}
	return binary.LittleEndian.Uint64(frame[start:end])
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
		for _, emitter := range stream.emitter {
			if emitter == nil {
				continue
			}
			if cerr := emitter.Close(); cerr != nil {
				closeErrs = append(closeErrs, cerr)
			}
		}
		for _, pw := range stream.pw {
			if cerr := pw.Close(); cerr != nil {
				closeErrs = append(closeErrs, cerr)
			}
		}
		err = errors.Join(closeErrs...)
	})

	return err
}

func StreamWithTTL(ttl time.Duration) StreamOption {
	return func(stream *Stream) {
		stream.ttlDuration = ttl
	}
}

func StreamWithAdapter(adapter io.ReadWriter) StreamOption {
	return func(stream *Stream) {
		stream.adapters = append(stream.adapters, adapter)
	}
}

func StreamWithRegions(n int) StreamOption {
	return func(stream *Stream) {
		if n < 1 {
			n = 1
		}

		for _, emitter := range stream.emitter {
			if emitter != nil {
				_ = emitter.Close()
			}
		}
		for _, pw := range stream.pw {
			_ = pw.Close()
		}

		stream.regions = n
		stream.rb = make([]*ringbuffer.RingBuffer, 0, n)
		stream.pr = make([]*ringbuffer.PipeReader, 0, n)
		stream.pw = make([]*ringbuffer.PipeWriter, 0, n)
		stream.frame = make([][]byte, 0, n)
		stream.emitter = make([]*Emitter, 0, n)
		stream.pending = make([]int, n)

		for range n {
			// Allocate a large enough ring buffer (e.g. 128MB) to hold the dataset without blocking.
			rb := ringbuffer.New(primitive.ByteSize * primitive.ByteSize * 128)
			pr, pw := rb.Pipe()
			stream.rb = append(stream.rb, rb)
			stream.pr = append(stream.pr, pr)
			stream.pw = append(stream.pw, pw)
			stream.frame = append(stream.frame, make([]byte, primitive.ByteSize))
			stream.emitter = append(stream.emitter, NewEmitter(pr, pw))
		}
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
