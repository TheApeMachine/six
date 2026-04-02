package vm

import (
	"context"
	"errors"
	"sync"

	"github.com/smallnest/ringbuffer"
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

var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, tokenizerChunkBytes())
	},
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
		buf := bufPool.Get().([]byte)
		defer bufPool.Put(buf)

		n, tokenizer.err = tokenizer.rb.Read(buf)

		value, err := primitive.NewValue(buf[:n])

		if err != nil {
			return 0, errnie.Error(err)
		}

		return value.Read(p)
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
		value, tokenizer.err = primitive.NewValue(p)

		if tokenizer.err != nil {
			return 0, errnie.Error(tokenizer.err)
		}

		var frame []byte

		frame, tokenizer.err = value.Bytes()

		if tokenizer.err != nil {
			return 0, errnie.Error(tokenizer.err)
		}

		n, tokenizer.err = tokenizer.rb.Write(frame)

		if tokenizer.err != nil {
			return n, errnie.Error(tokenizer.err)
		}

		return n, nil
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
