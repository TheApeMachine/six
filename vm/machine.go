package vm

import (
	"context"
	"runtime"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/validate"
)

/*
Machine is the top-level VM: Loader ingests data; Think/Prompt produce output.
Start() runs ingestion (substrate); Think and Prompt resolve through the Backend.
*/
type Machine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	loader  *Loader
	pool    *pool.Pool
	backend kernel.Backend
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine. Use MachineWithLoader, MachineWithPool,
MachineWithBackend to configure. Pool defaults to pool.New() if nil.
Backend defaults to kernel.NewBackend() (from SIX_BACKEND env) if nil.
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

	if machine.pool == nil {
		machine.pool = pool.New(
			context.Background(),
			1,
			runtime.NumCPU(),
			pool.NewConfig(),
		)
	}

	if machine.backend == nil {
		b, err := kernel.NewBackend()
		if err != nil {
			panic("failed to initialize backend: " + err.Error())
		}
		machine.backend = b
	}

	validate.Require(map[string]any{
		"backend": machine.backend,
		"loader":  machine.loader,
		"pool":    machine.pool,
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
Prompt resolves prompt chords through the Backend for O(1) suffix recall.
Returns a channel of chord sequences.
*/
func (machine *Machine) Prompt(prompt []data.Chord) chan []data.Chord {
	out := make(chan []data.Chord)

	go func() {
		defer close(out)

		if len(prompt) == 0 {
			return
		}

		results, err := machine.backend.Resolve(prompt)

		if err != nil {
			return
		}

		var output []data.Chord

		for _, packed := range results {
			idx, score := kernel.DecodePacked(packed)

			if idx >= 0 && score > 0 {
				output = append(output, prompt[idx%len(prompt)])
			}
		}

		if len(output) > 0 {
			out <- output
		}
	}()

	return out
}

/*
Backend returns the configured Backend for external access.
*/
func (machine *Machine) Backend() kernel.Backend {
	return machine.backend
}

/*
WithContext adds a context to the machine.
*/
func (machine *Machine) WithContext(ctx context.Context) machineOpts {
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
MachineWithPool sets the worker pool for parallel suffix construction in Start.
*/
func MachineWithPool(p *pool.Pool) machineOpts {
	return func(machine *Machine) {
		machine.pool = p
	}
}

/*
MachineWithBackend sets the kernel backend for chord resolution.
*/
func MachineWithBackend(b kernel.Backend) machineOpts {
	return func(machine *Machine) {
		machine.backend = b
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
func (e MachineError) Error() string {
	return string(e)
}
