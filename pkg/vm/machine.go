package vm

import (
	"context"
	"io"
	"unsafe"

	"github.com/theapemachine/six/pkg/cluster"
	"github.com/theapemachine/six/pkg/compute"
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
	backend      *compute.Backend
	sources      io.Reader
	destinations io.Writer
	controlplane *cluster.ControlPlane
	prevID       uint64
	nextID       uint64
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
	ctx, cancel := context.WithCancel(ctx)

	machine = &Machine{
		ctx:          ctx,
		cancel:       cancel,
		backend:      compute.NewBackend(ctx),
		controlplane: cluster.NewControlPlane(ctx),
	}

	tokenizer, err := NewTokenizer(ctx)

	if err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, err),
		)
	}

	/*
		Wire tokenizer as the sole initial source/destination. Do not pass nil into
		io.MultiReader/io.MultiWriter: a nil slot panics on Read/Write (see std io/multi.go).
	*/
	machine.sources = tokenizer
	machine.destinations = tokenizer

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":          machine.ctx,
		"cancel":       machine.cancel,
		"sources":      machine.sources,
		"destinations": machine.destinations,
		"backend":      machine.backend,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, machine.err),
		)
	}

	machine.start()

	return machine, nil
}

/*
start the machine as an infinite loop by making it read from itself, and
write to itself, which will automatically sequence the
io.MultiReader and io.MultiWriter, and create a seamless feedback loop.
*/
func (machine *Machine) start() (err error) {
	go func() {
		for {
			select {
			case <-machine.ctx.Done():
				return
			default:
				if _, machine.err = io.Copy(machine, machine); machine.err != nil {
					machine.err = errnie.Error(
						NewMachineError(ErrStreamFailed, machine.err),
					)
				}
			}
		}
	}()

	return nil
}

/*
Read implements io.Reader so the machine acts as a wiring mechanism between
sources and destinations. These are explicitely MultiReader and MultiWriter
objects, so they can be used to connect multiple sources and destinations
via the machine.
*/
func (machine *Machine) Read(p []byte) (n int, err error) {
	select {
	case <-machine.ctx.Done():
		return 0, machine.ctx.Err()
	default:
		value := primitive.BytesToValue(p)
		machine.controlplane.Insert(*value)
		machine.backend.Queue(unsafe.Pointer(value))

		return machine.sources.Read(p)
	}
}

/*
Write implements io.Writer so the machine acts as a wiring mechanism between
sources and destinations. These are explicitely MultiReader and MultiWriter
objects, so they can be used to connect multiple sources and destinations
via the machine.
*/
func (machine *Machine) Write(p []byte) (n int, err error) {
	if machine.destinations == nil {
		return len(p), nil
	}

	return machine.destinations.Write(p)
}

/*
Close implements io.Closer so the machine can be closed, which will cancel
the context, and if everything is wired up correctly, this should trigger
a full system-wide shutdown. This means that the system's context must be
the ultimate root context for the system.
*/
func (machine *Machine) Close() (err error) {
	if machine.cancel != nil {
		machine.cancel()
	}

	return err
}

/*
WithSources configures the machine with one or more sources, which act as
the ingress points for data.
*/
func WithSources(readers ...io.Reader) machineOption {
	return func(machine *Machine) {
		machine.sources = io.MultiReader(
			append([]io.Reader{machine.sources}, readers...)...,
		)
	}
}

/*
WithDestinations configures the machine with one or more destinations,
which act as the egress points for data.
*/
func WithDestinations(writers ...io.Writer) machineOption {
	return func(machine *Machine) {
		machine.destinations = io.MultiWriter(
			append([]io.Writer{machine.destinations}, writers...)...,
		)
	}
}
