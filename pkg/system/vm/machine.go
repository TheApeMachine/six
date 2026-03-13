package vm

import (
	"bytes"
	"context"
	"runtime"
	"time"

	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
Machine is the top-level VM: Loader ingests data; the Cortex reasons.
Tick receives broadcast messages and dispatches work through the qpool.
Pool and Broadcast are injected by the Booter; Machine never creates its own.
*/
type Machine struct {
	ctx            context.Context
	cancel         context.CancelFunc
	systems        []System
	cortex         Cortex
	decoder        Decoder
	workerPool     *pool.Pool
	broadcastGroup *pool.BroadcastGroup
	booterCtx      context.Context
	booterCancel   context.CancelFunc
	booterDone     chan struct{}
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		booterDone: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

/*
Start boots the full system: creates pool and broadcast, starts all systems,
runs the Booter event loop in a background goroutine, and blocks until
all Readiness-implementing systems report ready (or timeout).
*/
func (machine *Machine) Start() {
	machine.workerPool = pool.New(
		machine.ctx,
		1,
		runtime.NumCPU(),
		&pool.Config{},
	)

	machine.broadcastGroup = pool.NewBroadcastGroup(
		"broadcast", 10*time.Second, 128,
	)

	for _, system := range machine.systems {
		system.Start(machine.workerPool, machine.broadcastGroup)
	}

	for _, system := range machine.systems {
		if cortexSys, ok := system.(Cortex); ok {
			machine.cortex = cortexSys
		}

		if decoderSys, ok := system.(Decoder); ok {
			machine.decoder = decoderSys
		}
	}

	machine.booterCtx, machine.booterCancel = context.WithCancel(machine.ctx)

	booter := NewBooter(
		BooterWithContext(machine.booterCtx),
		BooterWithPool(machine.workerPool),
		BooterWithBroadcast(machine.broadcastGroup),
		BooterWithSystems(machine.systems...),
	)

	go func() {
		booter.Start()
		close(machine.booterDone)
	}()

	machine.waitReady()
}

/*
waitReady polls all systems that implement Readiness until they report
true. Gives up after 30 seconds to avoid hanging in broken setups.
*/
func (machine *Machine) waitReady() {
	deadline := time.After(30 * time.Second)

	for {
		allReady := true

		for _, system := range machine.systems {
			if sysReady, ok := system.(Readiness); ok && !sysReady.Ready() {
				allReady = false
				break
			}
		}

		if allReady {
			return
		}

		select {
		case <-deadline:
			return
		case <-machine.ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

/*
Prompt sends a ChordSource through the full end-to-end pipeline:
chords → cortex (matrix) → result chords → decoder (LSM) → bytes.
Returns the reconstructed byte sequences.
*/
func (machine *Machine) Prompt(source ChordSource) ([][]byte, error) {
	if machine.cortex == nil {
		return nil, ErrNoCortex
	}

	if machine.decoder == nil {
		return nil, ErrNoDecoder
	}

	var allResults [][]byte

	for source.Next() {
		if source.Error() != nil {
			return nil, source.Error()
		}

		chords := source.Chords()

		if len(chords) == 0 {
			continue
		}

		list, err := data.ChordSliceToList(chords)

		if err != nil {
			return nil, err
		}

		paths, err := machine.cortex.PromptChords(machine.ctx, list)

		console.Trace("machine", "paths", paths)

		if err != nil {
			return nil, err
		}

		for _, path := range paths {
			decoded := machine.decoder.Decode(path)
			allResults = append(allResults, decoded...)
		}
	}

	console.Trace("machine", "output", string(bytes.Join(allResults, []byte{})))

	return allResults, source.Error()
}

/*
Pool returns the Machine's worker pool, so external components
(like a Prompt's tokenizer) can be started on the same pool.
*/
func (machine *Machine) Pool() *pool.Pool {
	return machine.workerPool
}

/*
Stop cancels the Machine context and closes the worker pool.
*/
func (machine *Machine) Stop() {
	if machine.cancel != nil {
		machine.cancel()
	}

	if machine.booterCancel != nil {
		machine.booterCancel()
		<-machine.booterDone
	}

	if machine.broadcastGroup != nil {
		machine.broadcastGroup.Close()
	}

	if machine.workerPool != nil {
		machine.workerPool.Close()
	}
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
MachineWithSystems assigns an arbitrary set of system interfaces
which are wired into the underlying Event Loop and worker Pool on start.
*/
func MachineWithSystems(systems ...System) machineOpts {
	return func(machine *Machine) {
		machine.systems = systems
	}
}

/*
MachineError is a typed error for Machine failures.
*/
type MachineError string

const (
	ErrNoChordFound                MachineError = "no chord found"
	ErrMultiScaleCooccurrenceBuild MachineError = "failed to build multiscale cooccurrence"
	ErrNoCortex                    MachineError = "no cortex system registered"
	ErrNoDecoder                   MachineError = "no decoder system registered"
)

/*
Error implements the error interface for MachineError.
*/
func (machineError MachineError) Error() string {
	return string(machineError)
}
