package vm

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
)

var bufPool = sync.Pool{
	New: func() any {
		return make(
			[]byte,
			int(core.Cfg.Value.Region.Tokens.Bits/8),
		)
	},
}

/*
wireFrameBufPool holds buffers sized for Value.Bytes egress when a Pipeline
tees wire frames while Publish runs on live *Value instances.
*/
var wireFrameBufPool = sync.Pool{
	New: func() any {
		return make([]byte, core.Cfg.Value.Bytes)
	},
}

type Tokenizer struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	rb     *ringbuffer.RingBuffer
	pr     *ringbuffer.PipeReader
	pw     *ringbuffer.PipeWriter
	// pipeWriterClosed gates ClosePipeWriter so Load can unblock the ingest
	// side when the drain path fails while the ring is full.
	pipeWriterClosed uint32
	queue            *pool.Queue
	current          *primitive.Value
}

type tokenizerOption func(*Tokenizer)

func NewTokenizer(
	ctx context.Context,
	queue *pool.Queue,
	opts ...tokenizerOption,
) (*Tokenizer, error) {
	if err := validate.Require(map[string]any{
		"queue": queue,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	ctx, cancel := context.WithCancel(ctx)

	rb := ringbuffer.New(
		int(
			(core.Cfg.Value.Region.Tokens.Bits / 8) * uint64(core.Cfg.Value.Bytes),
		),
	)

	pr, pw := rb.Pipe()

	tokenizer := &Tokenizer{
		ctx:    ctx,
		cancel: cancel,
		rb:     rb,
		pr:     pr,
		pw:     pw,
		queue:  queue,
	}

	for _, opt := range opts {
		opt(tokenizer)
	}

	return tokenizer, validate.Require(map[string]any{
		"tokenizer": tokenizer,
		"ctx":       tokenizer.ctx,
		"cancel":    tokenizer.cancel,
		"rb":        tokenizer.rb,
		"queue":     tokenizer.queue,
	})
}

func (tokenizer *Tokenizer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	buf := bufPool.Get().([]byte)
	buf = buf[:cap(buf)]
	defer bufPool.Put(buf[:0])

	rbN, rbErr := tokenizer.rb.Read(buf)

	if rbN > 0 {
		if _, adoptErr := tokenizer.adoptChunk(buf[:rbN]); adoptErr != nil {
			return 0, adoptErr
		}

		frameN, frameErr := tokenizer.current.Read(p)

		// Value.Read returns io.EOF after each full frame (single-shot Reader).
		// That is not tokenizer end-of-stream while the ring still has bytes.
		if frameN > 0 && errors.Is(frameErr, io.EOF) {
			frameErr = nil
		}

		if rbErr != nil && frameN > 0 && errors.Is(rbErr, io.EOF) {
			rbErr = nil
		}

		if rbErr != nil {
			return frameN, errnie.Error(rbErr)
		}

		return frameN, frameErr
	}

	if rbErr != nil {
		return 0, errnie.Error(rbErr)
	}

	return 0, nil
}

/*
adoptChunk mints tokenizer.current from one raw ingest chunk and links the
doubly-linked ID chain across successive Values. The previous current is
closed when replaced.
*/
func (tokenizer *Tokenizer) adoptChunk(chunk []byte) (*primitive.Value, error) {
	old := tokenizer.current

	next, err := primitive.NewValue(chunk)

	if err != nil {
		tokenizer.err = err

		return nil, errnie.Error(err)
	}

	if old != nil {
		next.Set(
			core.Cfg.Value.Region.Prev.Start,
			old.ID(),
		)

		old.Set(
			core.Cfg.Value.Region.Next.Start,
			next.ID(),
		)

		_ = old.Close()
	}

	tokenizer.current = next

	return next, nil
}

/*
DrainPublishedValues drains raw ingest from the ring into NewValue-sized
chunks, publishes each live *Value to every sink, and optionally writes one
wire frame per value to frameTee for Pipeline egress.

It mirrors Read's chunking and prev/next linking without ValueFromWireFrame.
*/
func (tokenizer *Tokenizer) DrainPublishedValues(
	ctx context.Context,
	label string,
	publishers []transport.Publishable,
	frameTee io.Writer,
) (err error) {
	if tokenizer == nil {
		return errnie.Error(errors.New("vm.Tokenizer.DrainPublishedValues: nil Tokenizer"))
	}

	buf := bufPool.Get().([]byte)
	buf = buf[:cap(buf)]
	defer bufPool.Put(buf[:0])

	frameNeed := core.Cfg.Value.Bytes

	var frameBacking []byte

	if frameTee != nil {
		raw := wireFrameBufPool.Get().([]byte)
		raw = raw[:cap(raw)]

		if cap(raw) < frameNeed {
			raw = make([]byte, frameNeed)
		}

		frameBacking = raw

		defer func() {
			wireFrameBufPool.Put(raw)
		}()
	}

	for {
		if ctx != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}

		rbN, rbErr := tokenizer.rb.Read(buf)

		if rbN > 0 {
			next, adoptErr := tokenizer.adoptChunk(buf[:rbN])

			if adoptErr != nil {
				return adoptErr
			}

			for _, publisher := range publishers {
				if pubErr := publisher.Publish(next, label); pubErr != nil {
					return pubErr
				}
			}

			if frameTee != nil {
				frameBuf := frameBacking[:frameNeed]
				frameN, readErr := next.Read(frameBuf)

				if frameN > 0 && errors.Is(readErr, io.EOF) {
					readErr = nil
				}

				if readErr != nil {
					return errnie.Error(readErr)
				}

				if _, wErr := frameTee.Write(frameBuf[:frameN]); wErr != nil {
					return wErr
				}
			}

			if rbErr != nil {
				if errors.Is(rbErr, io.EOF) {
					return nil
				}

				return errnie.Error(rbErr)
			}

			continue
		}

		if rbErr != nil {
			if errors.Is(rbErr, io.EOF) {
				return nil
			}

			return errnie.Error(rbErr)
		}

		return nil
	}
}

func (tokenizer *Tokenizer) Write(p []byte) (n int, err error) {
	return tokenizer.pw.Write(p)
}

/*
CloseWrite satisfies transport.FramedBytePipe: end the raw ingest side after
io.Copy from the dataset finishes.
*/
func (tokenizer *Tokenizer) CloseWrite() error {
	return tokenizer.ClosePipeWriter()
}

var _ transport.FramedBytePipe = (*Tokenizer)(nil)

/*
ClosePipeWriter signals end-of-stream to readers so blocked Read calls
unblock after the ring drains. Pair with ResetAfterEOF before reuse.
*/
func (tokenizer *Tokenizer) ClosePipeWriter() error {
	if tokenizer == nil || tokenizer.pw == nil {
		return nil
	}

	if !atomic.CompareAndSwapUint32(&tokenizer.pipeWriterClosed, 0, 1) {
		return nil
	}

	return tokenizer.pw.Close()
}

/*
ResetAfterEOF clears tokenizer-side state and resets the ring after the
write end was closed and reads have drained. The same PipeReader and
PipeWriter handles remain valid for the next ingest.
*/
func (tokenizer *Tokenizer) ResetAfterEOF() {
	if tokenizer == nil || tokenizer.rb == nil {
		return
	}

	if tokenizer.current != nil {
		_ = tokenizer.current.Close()
		tokenizer.current = nil
	}

	atomic.StoreUint32(&tokenizer.pipeWriterClosed, 0)
	tokenizer.rb.Reset()
}

func (tokenizer *Tokenizer) Close() (err error) {
	tokenizer.cancel()

	var curClose error

	if tokenizer.current != nil {
		curClose = tokenizer.current.Close()
		tokenizer.current = nil
	}

	return errors.Join(
		err,
		curClose,
		tokenizer.pw.Close(),
		tokenizer.pr.Close(),
	)
}

var _ transport.PublishedValueDrainer = (*Tokenizer)(nil)
