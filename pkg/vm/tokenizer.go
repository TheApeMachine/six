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

var bufPool = sync.Pool{
	New: func() any {
		return make(
			[]byte, 0, core.Cfg.Value.Region.Tokens.Bits,
		)
	},
}

type Tokenizer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	rb      *ringbuffer.RingBuffer
	pr      *ringbuffer.PipeReader
	pw      *ringbuffer.PipeWriter
	pool    *compute.Pool
	current *primitive.Value
}

type tokenizerOption func(*Tokenizer)

func NewTokenizer(
	ctx context.Context, opts ...tokenizerOption,
) (*Tokenizer, error) {
	ctx, cancel := context.WithCancel(ctx)

	rb := ringbuffer.New(
		int(
			core.Cfg.Value.Region.Tokens.Bits * uint64(core.Cfg.Value.Bytes),
		),
	)

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

	return tokenizer, validate.Require(map[string]any{
		"tokenizer": tokenizer,
		"ctx":       tokenizer.ctx,
		"cancel":    tokenizer.cancel,
		"rb":        tokenizer.rb,
	})
}

func (tokenizer *Tokenizer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	var value *primitive.Value

	if tokenizer.current == nil {
		value = tokenizer.current
	}

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	if n, err = tokenizer.rb.Read(buf); err != nil {
		return n, errnie.Error(err)
	}

	tokenizer.current, tokenizer.err = primitive.NewValue(buf)

	if tokenizer.err != nil {
		return 0, errnie.Error(tokenizer.err)
	}

	if value != nil && tokenizer.current != nil {
		tokenizer.current.Set(
			core.Cfg.Value.Region.Prev.Start,
			value.ID(),
		)

		value.Set(
			core.Cfg.Value.Region.Next.Start,
			tokenizer.current.ID(),
		)

		tokenizer.current = value
	}

	return value.Read(p)
}

func (tokenizer *Tokenizer) Write(p []byte) (n int, err error) {
	return tokenizer.pw.Write(p)
}

func (tokenizer *Tokenizer) Close() (err error) {
	tokenizer.cancel()

	return errors.Join(
		err,
		tokenizer.pw.Close(),
		tokenizer.pr.Close(),
	)
}

func TokenizerWithPool(pool *compute.Pool) tokenizerOption {
	return func(tokenizer *Tokenizer) {
		tokenizer.pool = pool
	}
}
