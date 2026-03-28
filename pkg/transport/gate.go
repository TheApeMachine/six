package transport

import (
	"context"
	"io"
)

type GateState uint

const (
	GateStateOpen GateState = iota
	GateStateClosed
)

/*
Gate merges two ReadWriters: all reads and writes use left until left returns
EOF on read, then both switch to right (GateStateClosed). Nothing is read from
or written to right until left is exhausted (same sequencing as io.MultiReader
for reads).
*/
type Gate struct {
	left   io.ReadWriter
	right  io.ReadWriter
	ctx    context.Context
	cancel context.CancelFunc
	state  GateState
}

func NewGate(ctx context.Context, left, right io.ReadWriter) *Gate {
	ctx, cancel := context.WithCancel(ctx)

	return &Gate{
		ctx:    ctx,
		cancel: cancel,
		left:   left,
		right:  right,
		state:  GateStateOpen,
	}
}

func (gate *Gate) Read(p []byte) (n int, err error) {
	select {
	case <-gate.ctx.Done():
		return 0, gate.ctx.Err()
	default:
		if gate.state == GateStateOpen {
			n, err = gate.left.Read(p)

			if err == io.EOF {
				gate.state = GateStateClosed
			}

			return n, err
		}

		return gate.right.Read(p)
	}
}

func (gate *Gate) Write(p []byte) (n int, err error) {
	select {
	case <-gate.ctx.Done():
		return 0, gate.ctx.Err()
	default:
		if gate.state == GateStateOpen {
			return gate.left.Write(p)
		}

		return gate.right.Write(p)
	}
}

func (gate *Gate) Close() error {
	gate.cancel()
	return nil
}
