package vm

import (
	"context"

	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/kernel/metal"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/validate"
)

/*
Machine is the top-level VM: Loader ingests data; the Cortex reasons.
Tick receives broadcast messages and dispatches work through the pool.
Pool and Broadcast are injected by the Booter; Machine never creates its own.
*/
type Machine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	broadcast *pool.BroadcastGroup
	loader    *Loader
	pool      *pool.Pool
	backend   kernel.Backend
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

	if machine.ctx == nil {
		ctx := context.Background()
		machine.ctx, machine.cancel = context.WithCancel(ctx)
	}

	if machine.backend == nil {
		machine.backend = kernel.NewBuilder(
			kernel.WithBackend(&metal.MetalBackend{}),
		)
	}

	validate.Require(map[string]any{
		"backend": machine.backend,
	})

	return machine
}

/*
Start runs ingestion: drains loader.Generate(), builds suffix entries per sample.
Requires loader to be set via MachineWithLoader.
*/
func (machine *Machine) Start() error {
	return machine.loader.Start()
}

/*
Stop terminates the Machine and signals any background processes to finish.
*/
func (machine *Machine) Stop() {
	machine.cancel()
}

/*
Tick processes a broadcast Result and dispatches work through the pool.
This is the System interface implementation. No goroutines are spawned here;
all work runs inside pool.Schedule.
*/
func (machine *Machine) Tick(result *pool.Result) {
}

/*
Backend returns the configured Backend for external access.
*/
func (machine *Machine) Backend() kernel.Backend {
	return machine.backend
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
MachineWithLoader sets the Loader for ingestion. Required for Start.
*/
func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}

/*
MachineWithPool injects the shared worker pool from the Booter.
*/
func MachineWithPool(workerPool *pool.Pool) machineOpts {
	return func(machine *Machine) {
		machine.pool = workerPool
	}
}

/*
MachineWithBackend sets the kernel backend for chord resolution.
*/
func MachineWithBackend(backend kernel.Backend) machineOpts {
	return func(machine *Machine) {
		machine.backend = backend
	}
}

/*
MachineWithBroadcast sets the broadcast group for inter-system messaging.
*/
func MachineWithBroadcast(broadcast *pool.BroadcastGroup) machineOpts {
	return func(machine *Machine) {
		machine.broadcast = broadcast
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
