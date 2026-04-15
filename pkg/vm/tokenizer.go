package vm

import (
	"context"
	"errors"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

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

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.TokenizerChunkEvent(len(sample.Text)))
	}

	segments, err := primitive.NewValue(sample.Text, sample.Label)

	if err != nil {
		return nil, errnie.Error(err)
	}

	if telemetry.DefaultBus.IsActive() {
		for _, segment := range segments {
			telemetry.PublishWireValueFrame(segment.ID(), segment.Bytes())
			telemetry.DefaultBus.Publish(telemetry.TokenizerEmitEvent(segment, string(sample.Label)))
		}
	}

	return segments, nil
}
