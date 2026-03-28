package transport

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
)

type GateState uint32

const (
	GateStateOpen GateState = iota
	GateStateClosed
)

/*
Gate merges two ReadWriters: all reads and writes use left until left returns
EOF on read, then both switch to right (GateStateClosed). Nothing is read from
or written to right until left is exhausted (same sequencing as io.MultiReader
for reads).

Gate.state is accessed with atomics so concurrent Read/Write do not race.

Close cancels the gate context and closes left/right when they implement io.Closer.
*/
type Gate struct {
	left   io.ReadWriter
	right  io.ReadWriter
	ctx    context.Context
	cancel context.CancelFunc
	state  atomic.Uint32
}

func NewGate(ctx context.Context, left, right io.ReadWriter) *Gate {
	ctx, cancel := context.WithCancel(ctx)

	g := &Gate{
		ctx:    ctx,
		cancel: cancel,
		left:   left,
		right:  right,
	}
	g.state.Store(uint32(GateStateOpen))
	return g
}

func (gate *Gate) Read(p []byte) (n int, err error) {
	select {
	case <-gate.ctx.Done():
		return 0, gate.ctx.Err()
	default:
		if gate.state.Load() == uint32(GateStateOpen) {
			n, err = gate.left.Read(p)

			if err == io.EOF {
				gate.state.Store(uint32(GateStateClosed))
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
		if gate.state.Load() == uint32(GateStateOpen) {
			return gate.left.Write(p)
		}

		return gate.right.Write(p)
	}
}

func (gate *Gate) Close() error {
	gate.cancel()

	var errs []error
	if c, ok := gate.left.(io.Closer); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c, ok := gate.right.(io.Closer); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
