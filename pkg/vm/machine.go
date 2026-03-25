package vm

import (
	"context"
	"io"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/workflow"
)

/*
Machine is purely a convenience wrapper around a workflow,
which in itself if just a convenience wrapper around Value types.
The idea is that Values pass through Values, which activates the
second property of the Value type: behavior.
Machine reduces boilerplate, but is not an essential part of the
system's operational mechanics.
*/
type Machine struct {
	ctx      context.Context
	cancel   context.CancelFunc
	dataset  io.ReadCloser
	backend  io.ReadWriteCloser
	prompt   io.ReadWriteCloser
	pipeline io.ReadWriter
}

/*
machineOption is a function that can be used to configure a Machine.
*/
type machineOption func(*Machine)

/*
NewMachine creates a new Machine with the given options.
*/
func NewMachine(opts ...machineOption) *Machine {
	machine := &Machine{
		backend: compute.NewBackend(),
		prompt:  primitive.NewValue(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	machine.pipeline = workflow.NewPipeline(
		workflow.NewSeeder(machine.dataset),
		primitive.NewValue(),
		machine.backend,
	)

	return machine
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	n, err = machine.pipeline.Read(p)
	errnie.Trace("vm.machine.Read", "n", n, "err", err)
	return n, err
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	n, err = machine.pipeline.Write(p)
	errnie.Trace("vm.machine.Write", "n", n, "err", err)
	return n, err
}

func (machine *Machine) Close() error {
	machine.cancel()
	return nil
}

func WithContext(ctx context.Context) machineOption {
	return func(m *Machine) {
		m.ctx, m.cancel = context.WithCancel(ctx)
	}
}

func WithBackend(backend io.ReadWriteCloser) machineOption {
	return func(m *Machine) {
		m.backend = backend
	}
}

func WithDataset(dataset io.ReadCloser) machineOption {
	return func(m *Machine) {
		m.dataset = dataset
	}
}
