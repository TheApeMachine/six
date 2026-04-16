package vm

import (
	"context"

	"github.com/theapemachine/six/pkg/primitive"
)

type Emitter struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	values []*primitive.Value
}

func NewEmitter(ctx context.Context) (*Emitter, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &Emitter{
		ctx:    ctx,
		cancel: cancel,
		values: make([]*primitive.Value, 0),
	}, nil
}

func (emitter *Emitter) Close() error {
	emitter.cancel()
	return emitter.err
}

func (emitter *Emitter) Error() error {
	return emitter.err
}

func (emitter *Emitter) Write(p []byte) (int, error) {
	value, err := primitive.ValueFromWireFrame(p)
	if err != nil {
		return 0, err
	}

	emitter.values = append(emitter.values, value)

	return len(p), nil
}
