package vm

import (
	"bytes"
	"context"
	"runtime"
	"time"

	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
)

/*
Cortex is any System that can accept chords and return paths through the
real Prompt→SpatialLookup→Evaluate→RecursiveFold pipeline.
*/
type Cortex interface {
	System
	PromptChords(ctx context.Context, chords data.Chord_List) ([][]data.Chord, error)
}

/*
Decoder converts result chords back to original byte sequences
by reversing the LSM encoding (extracting byte values from entry keys).
*/
type Decoder interface {
	Decode(chords []data.Chord) [][]byte
}

/*
ChordSource produces chord batches for Machine.Prompt. Each call to Next()
advances to the next sample; Chords() returns the chords for that sample.
process.Prompt already satisfies this interface.
*/
type ChordSource interface {
	Next() bool
	Chords() []data.Chord
	Error() error
}

/*
Readiness is an optional interface a System can implement to signal
that its initial data ingestion is complete.
*/
type Readiness interface {
	Ready() bool
}

/*
Machine is the top-level VM: Loader ingests data; the Cortex reasons.
Tick receives broadcast messages and dispatches work through the qpool.
Pool and Broadcast are injected by the Booter; Machine never creates its own.
*/
type Machine struct {
	ctx        context.Context
	cancel     context.CancelFunc
	systems    []System
	cortex     Cortex
	decoder    Decoder
	workerPool *pool.Pool
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{}

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

	broadcast := pool.NewBroadcastGroup(
		"broadcast", 10*time.Second, 128,
	)

	for _, system := range machine.systems {
		system.Start(machine.workerPool, broadcast)
	}

	for _, system := range machine.systems {
		if c, ok := system.(Cortex); ok {
			machine.cortex = c
		}

		if d, ok := system.(Decoder); ok {
			machine.decoder = d
		}
	}

	booter := NewBooter(
		BooterWithContext(machine.ctx),
		BooterWithPool(machine.workerPool),
		BooterWithBroadcast(broadcast),
		BooterWithSystems(machine.systems...),
	)

	go booter.Start()

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
			if r, ok := system.(Readiness); ok && !r.Ready() {
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
	machine.cancel()
	machine.workerPool.Close()
}

/*
MachineWithContext adds a context to the machine.
*/
func MachineWithContext(ctx context.Context) machineOpts {
	return func(machine *Machine) {
		machine.ctx, machine.cancel = context.WithCancel(ctx)
	}
}

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
