package vm

import (
	"bufio"
	"context"
	"io"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Machine provides a unified stream processing pipeline. All machine options are
applied during construction so tests and callers can instantiate isolated
machines without relying on package-level state.
*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	sources      io.Reader
	destinations io.Writer
	values       []*primitive.Value
}

type machineOption func(*Machine)

/*
NewMachine constructs a new Machine with the provided options.
It requires a context for lifecycle management and will return
an error if the context is invalid or if the underlying stream
fails to start. The machine can be configured with various options,
such as custom datasets, stream adapters, and region counts.
*/
func NewMachine(
	ctx context.Context, opts ...machineOption,
) (machine *Machine, err error) {
	if ctx == nil {
		return nil, errnie.Error(
			NewMachineError(ErrNoContext),
		)
	}

	machine = &Machine{}
	machine.ctx, machine.cancel = context.WithCancel(ctx)

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":     machine.ctx,
		"cancel":  machine.cancel,
		"sources": machine.sources,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, machine.err),
		)
	}

	machine.start()

	return machine, nil
}

func (machine *Machine) start() (err error) {
	scanner := bufio.NewScanner(machine.sources)
	buf := make([]byte, 0, core.Cfg.ValueSize/128)

	for {
		select {
		case <-machine.ctx.Done():
			return nil
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return errnie.Error(
						NewMachineError(ErrDatasetNotClosed, err),
					)
				}

				return nil
			}

			scanner.Buffer(buf, len(buf))

			var value *primitive.Value

			if value, err = primitive.NewValue(buf); err != nil {
				return errnie.Error(
					NewMachineError(ErrValueError, err),
				)
			}

			machine.values = append(machine.values, value)
			buf = buf[:0]
		}
	}
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.sources.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	if machine.destinations == nil {
		return len(p), nil
	}

	return machine.destinations.Write(p)
}

func (machine *Machine) Close() (err error) {
	if machine.cancel != nil {
		machine.cancel()
	}

	return err
}

func WithSources(readers io.ReadCloser) machineOption {
	return func(machine *Machine) {
		machine.sources = io.LimitReader(io.MultiReader(
			readers,
		), int64(core.Cfg.ValueSize))
	}
}

func WithDestinations(writers io.WriteCloser) machineOption {
	return func(machine *Machine) {
		machine.destinations = io.MultiWriter(
			writers,	
		)
	}
}