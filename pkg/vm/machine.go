package vm

import (
	"context"
)

/*
Machine is the top-level VM: Loader ingests data; the Cortex reasons.
Tick receives broadcast messages and dispatches work through the qpool.
Pool and Broadcast are injected by the Booter; Machine never creates its own.
*/
type Machine struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
Pool must be injected via MachineWithPool (from Booter).
Backend defaults to kernel.NewBuilder if nil.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

/*
MachineWithContext adds a context to the machine.
*/
func MachineWithContext(ctx context.Context) machineOpts {
	return func(machine *Machine) {
		machine.ctx, machine.cancel = context.WithCancel(ctx)
	}
}

/*
MachineError is a typed error for Machine failures.
*/
type MachineError string

const (
	ErrNoChordFound                MachineError = "no chord found"
	ErrMultiScaleCooccurrenceBuild MachineError = "failed to build multiscale cooccurrence"
)

/*
Error implements the error interface for MachineError.
*/
func (machineError MachineError) Error() string {
	return string(machineError)
}
