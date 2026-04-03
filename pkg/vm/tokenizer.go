package vm

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/cluster"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
tokenizerChunkBytes returns the ingest chunk size derived from the live config.
It must not run at package init because viper-backed Cfg is populated after
TestMain (and Defaults would yield 0 bits before subtracting headers).
*/
var (
	tokenizerChunkOnce sync.Once
	tokenizerChunkSize int
)

func computeTokenizerChunkBytes() int {
	raw := int(core.Cfg.Value.Region.Tokens.Bits/8) - 3*8
	if raw < 1 {
		return 1
	}

	return raw
}

func tokenizerChunkBytes() int {
	tokenizerChunkOnce.Do(func() {
		tokenizerChunkSize = computeTokenizerChunkBytes()
	})

	return tokenizerChunkSize
}

// TokenizerChunkBytes exposes the active ingest chunk size used by the tokenizer.
// This is required by prompt injection helpers that must mirror tokenizer payload
// packing exactly.
func TokenizerChunkBytes() int {
	return tokenizerChunkBytes()
}

/*
Tokenizer takes a raw byte stream, chunks the incoming data to
match the token region size, and produces the canonical Values.
*/
type Tokenizer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	rb      *ringbuffer.RingBuffer
	pr      *ringbuffer.PipeReader
	pw      *ringbuffer.PipeWriter
	backend *compute.Backend
	store   *cluster.ControlPlane
}

type TokenizerOpts func(*Tokenizer)

/*
NewTokenizer creates a new Tokenizer, and sets up a default buffer
of 128 token region-sized sequences.
*/
func NewTokenizer(
	ctx context.Context,
	opts ...TokenizerOpts,
) (*Tokenizer, error) {
	ctx, cancel := context.WithCancel(ctx)
	rb := ringbuffer.New(tokenizerChunkBytes() * 128)
	pr, pw := rb.Pipe()

	tokenizer := &Tokenizer{
		ctx:    ctx,
		cancel: cancel,
		rb:     rb,
		pr:     pr,
		pw:     pw,
	}

	for _, opt := range opts {
		opt(tokenizer)
	}

	if err := validate.Require(map[string]any{
		"ctx":     tokenizer.ctx,
		"cancel":  tokenizer.cancel,
		"rb":      tokenizer.rb,
		"pr":      tokenizer.pr,
		"pw":      tokenizer.pw,
		"backend": tokenizer.backend,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	tokenizer.rb = tokenizer.rb.WithCancel(tokenizer.ctx)
	return tokenizer, nil
}

/*
Tokenize tokenizes the given data into a stream of tokens.
*/
func (tokenizer *Tokenizer) Read(p []byte) (n int, err error) {
	select {
	case <-tokenizer.ctx.Done():
		return 0, tokenizer.ctx.Err()
	default:
		if len(p) < core.Cfg.Value.Bytes {
			return 0, io.ErrShortBuffer
		}

		if core.Cfg.Value.Bytes <= 0 {
			return 0, io.ErrShortBuffer
		}

		frame := p[:core.Cfg.Value.Bytes]
		_, tokenizer.err = io.ReadFull(tokenizer.pr, frame)
		if tokenizer.err != nil {
			return 0, errnie.Error(tokenizer.err)
		}

		return primitive.BytesToValue(frame).Read(p)
	}
}

/*
Write writes the given tokens to the given writer.
*/
func (tokenizer *Tokenizer) Write(p []byte) (n int, err error) {
	select {
	case <-tokenizer.ctx.Done():
		return 0, tokenizer.ctx.Err()
	default:
		var value *primitive.Value

		// Accept full serialized frame writes (e.g. vm.Write passthrough).
		// This keeps prompt metadata such as ID, link pointers, and firmware
		// marker intact when data is looped back through the machine.
		if len(p) == core.Cfg.Value.Bytes {
			value = primitive.BytesToValue(p)
		} else {
			// Otherwise treat input as raw payload and tokenize it into a frame.
			if value, tokenizer.err = primitive.NewValue(p); tokenizer.err != nil {
				return 0, errnie.Error(tokenizer.err)
			}
		}

		// Route the Value into the control plane by affinity. The LSM itself
		// still indexes the Value under its TokenIDs inside InsertBatch.
		if tokenizer.store != nil {
			key := value.GetWord(core.Cfg.Value.Region.Affinity.Start)
			tokenizer.store.Insert(key, *value)
		}

		var nn int64
		if nn, tokenizer.err = io.Copy(tokenizer.pw, value); tokenizer.err != nil {
			return 0, errnie.Error(tokenizer.err)
		}

		return int(nn), nil
	}
}

/*
Close closes the Tokenizer.
*/
func (tokenizer *Tokenizer) Close() (err error) {
	tokenizer.cancel()

	return errors.Join(
		tokenizer.err,
		tokenizer.ctx.Err(),
		tokenizer.pr.Close(),
		tokenizer.pw.Close(),
	)
}

func TokenizerWithBuffer(n int) TokenizerOpts {
	return func(tokenizer *Tokenizer) {
		tokenizer.rb = ringbuffer.New(tokenizerChunkBytes() * n)
		tokenizer.pr, tokenizer.pw = tokenizer.rb.Pipe()
	}
}

func TokenizerWithStore(store *cluster.ControlPlane) TokenizerOpts {
	return func(tokenizer *Tokenizer) {
		tokenizer.store = store
	}
}

/*
TokenizerWithBackend attaches the same compute Backend used for Machine.Queue.
The tokenizer’s Read/Write hot path does not enqueue work today; the field
exists for validation, future ingress-side execution, and wiring clarity.
*/
func TokenizerWithBackend(backend *compute.Backend) TokenizerOpts {
	return func(tokenizer *Tokenizer) {
		tokenizer.backend = backend
	}
}
