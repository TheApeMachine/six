package vm

import (
	"context"
	"errors"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Tokenizer turns a stream of data.Sample chunks into linked Value
segments. Successive IngestSample calls share a current cursor so the
previous batch's tail is wired to the new batch's head via
Prev / Next, matching the within-batch chaining primitive.NewValue
already does. Without this cross-call linking the visualiser sees a
forest of two-segment chains (one per chunk) instead of one
continuous causal graph for the input stream.
*/
type Tokenizer struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	current *primitive.Value
}

func NewTokenizer(ctx context.Context) (*Tokenizer, error) {
	ctx, cancel := context.WithCancel(ctx)

	tokenizer := &Tokenizer{
		ctx:    ctx,
		cancel: cancel,
	}

	return tokenizer, nil
}

func (tokenizer *Tokenizer) Close() error {
	tokenizer.cancel()
	return nil
}

func (tokenizer *Tokenizer) Error() error {
	return tokenizer.err
}

func (tokenizer *Tokenizer) IngestSample(
	ctx context.Context, sample data.Sample,
) ([]*primitive.Value, error) {
	if tokenizer == nil {
		return nil, errnie.Error(errors.New("vm.Tokenizer.IngestSample: nil Tokenizer"))
	}

	if len(sample.Text) == 0 {
		return nil, nil
	}

	segments, err := primitive.NewValue(sample.Text, sample.Label)

	if err != nil {
		return nil, errnie.Error(err)
	}

	if len(segments) == 0 {
		return segments, nil
	}

	if tokenizer.current != nil {
		prevStart := core.Cfg.Value.Region.Prev.Start
		nextStart := core.Cfg.Value.Region.Next.Start

		head := segments[0]
		tokenizer.current.Set(nextStart, head.ID())
		head.Set(prevStart, tokenizer.current.ID())
	}

	tokenizer.current = segments[len(segments)-1]

	return segments, nil
}
