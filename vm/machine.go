package vm

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/vm/cortex"
)

type bestFillFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

/*
Machine is the top-level VM: Loader ingests data; Think/Prompt produce output.
Start() runs ingestion (substrate + PrimeField), trains EigenMode, wires Sequencer.
Think uses the cortex graph; Prompt uses substrate suffix lookup.
*/
type Machine struct {
	ctx        context.Context
	cancel     context.CancelFunc
	loader     *Loader
	pool       *pool.Pool
	primefield *store.PrimeField
	substrate  *geometry.HybridSubstrate
	bestFill   bestFillFn
	eigenMode  *geometry.EigenMode
	sequencer  cortex.Analyzer
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine with PrimeField and kernel.BestFill. Use MachineWithLoader,
MachineWithPool to configure. Pool defaults to pool.New() if nil.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		bestFill:   kernel.BestFill,
	}

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

	return machine
}

/*
Start runs ingestion: drains loader.Generate(), builds suffix entries per sample,
merges into substrate and PrimeField, calls BuildEigenModes, wires Sequencer.
Requires loader to be set via MachineWithLoader.
*/
func (machine *Machine) Start() error {
	machine.substrate = geometry.NewHybridSubstrate()

	// Ensure the Loader has all internal dependencies.
	if machine.loader.store == nil {
		machine.loader.store = store.NewLSMSpatialIndex(1.0)
	}

	machine.loader.primefield = machine.primefield

	var sequence []data.Chord
	var bytesSeq []byte

	idx := 0
	wg := sync.WaitGroup{}

	for res := range machine.loader.Generate() {
		wg.Add(1)

		machine.pool.Schedule(fmt.Sprintf("loader-%d", idx), func() (any, error) {
			defer wg.Done()

			if res.IsBoundary {
				// Write suffix entries for the completed sample.
				rot := geometry.IdentityRotation()

				for i := 0; i < len(bytesSeq); i++ {
					var ptr data.Chord

					for range 5 {
						ptr.Set(rand.Intn(257))
					}

					machine.primefield.StorePointer(rot, ptr)

					suffix := make([]byte, len(bytesSeq)-i)
					copy(suffix, bytesSeq[i:])

					machine.substrate.Add(ptr, geometry.NewPhaseDial(), suffix)

					if i < len(bytesSeq) {
						rot = rot.Compose(geometry.RotationForByte(bytesSeq[i]))
					}
				}

				dial := geometry.NewPhaseDial()
				dial = dial.EncodeFromChords(sequence)

				machine.substrate.Add(
					data.Chord{},
					dial,
					[]byte("sequence"),
				)

				sequence = sequence[:0]
				bytesSeq = bytesSeq[:0]
			}

			if res.Chord.ActiveCount() > 0 {
				sequence = append(sequence, res.Chord)
				bytesSeq = append(bytesSeq, res.Symbol)
			}

			return nil, nil
		})
	}

	wg.Wait()

	// Build EigenModes from the ingested manifold topology.
	machine.primefield.BuildEigenModes()
	machine.eigenMode = machine.primefield.EigenMode()

	// Wire the Sequencer with the trained EigenMode.
	if machine.loader.Tokenizer() != nil && machine.loader.Tokenizer().Sequencer() != nil {
		seq := machine.loader.Tokenizer().Sequencer()
		seq.SetEigenMode(machine.eigenMode)
		machine.sequencer = seq
	}

	return nil
}

/*
Stop terminates the Machine and signals any background processes to finish.
*/
func (machine *Machine) Stop() {
	machine.cancel()
}

/*
Think creates a cortex Graph, injects prompt with Sequencer events, runs Tick()
until convergence, returns the sink's output channel. Uses PrimeField/Substrate
for recall. For bAbI-style extraction, see cortex.Think.
*/
func (machine *Machine) Think(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan []byte {
	cfg := cortex.Config{
		InitialNodes: 8,
		PrimeField:   machine.primefield,
		Substrate:    machine.substrate,
		BestFill:     kernel.BestFill,
		EigenMode:    machine.eigenMode,
		Sequencer:    machine.sequencer,
		MaxTicks:     256,
		MaxOutput:    512,
	}

	graph := cortex.New(cfg)
	return graph.Think(prompt, expectedReality)
}

/*
Prompt composes rot = RotationForByte for each prompt chord, calls
HolographicRecall(filters, manifolds, rot), streams the matched suffix bytes.
No cortex; O(1) suffix lookup via GF(257) rotational addressing.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan []byte {
	out := make(chan []byte)

	go func() {
		defer close(out)

		rot := geometry.IdentityRotation()
		filters := machine.substrate.Filters()

		if len(filters) > 0 {
			match, err := kernel.HolographicRecall(
				filters,
				machine.primefield.ManifoldsPtr(),
				rot,
			)

			if err == nil && match.Index >= 0 && match.Score > 0 {
				out <- machine.substrate.Entries[match.Index].Readout
			}
		}
	}()

	return out
}

/*
Substrate returns the HybridSubstrate populated by Start.
*/
func (machine *Machine) Substrate() *geometry.HybridSubstrate {
	return machine.substrate
}

/*
WithContext adds a context to the machine
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
