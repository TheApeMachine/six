package vm

import (
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
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
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Generation uses the reactive cortex graph — a volatile
working-memory network of resonating MacroCubes that vibrates
until convergence, emitting output via BestFace decoding.
*/
type Machine struct {
	loader     *Loader
	primefield *store.PrimeField
	substrate  *geometry.HybridSubstrate
	bestFill   bestFillFn
	stopCh     chan struct{}
	eigenMode  *geometry.EigenMode
	sequencer  cortex.Analyzer
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		bestFill:   kernel.BestFill,
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

func (machine *Machine) Start() error {
	machine.stopCh = make(chan struct{})
	machine.substrate = geometry.NewHybridSubstrate()

	// Ensure the Loader has all internal dependencies.
	if machine.loader.store == nil {
		machine.loader.store = store.NewLSMSpatialIndex(1.0)
	}

	machine.loader.primefield = machine.primefield

	var sequence []data.Chord
	for res := range machine.loader.Generate() {
		sequence = append(sequence, res.Chord)

		if res.IsBoundary {
			// Sequence complete: encode as PhaseDial and add to substrate.
			// We use a zero filter for now as experiments typically use the
			// fingerprint directly. Readout is currently the segment index.
			dial := geometry.NewPhaseDial()
			dial = dial.EncodeFromChords(sequence)

			machine.substrate.Add(
				data.Chord{},
				dial,
				[]byte("sequence"), // Readout will be refined later
			)

			sequence = sequence[:0]
		}
	}

	// Flush trailing chords that arrived after the last boundary.
	if len(sequence) > 0 {
		dial := geometry.NewPhaseDial()
		dial = dial.EncodeFromChords(sequence)

		machine.substrate.Add(
			data.Chord{},
			dial,
			[]byte("sequence"),
		)
	}

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
	if machine.stopCh != nil {
		close(machine.stopCh)
		machine.stopCh = nil
	}
}

/*
Prompt generates output from the prompt using the cortex.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	var stopCh <-chan struct{}
	
	if machine.stopCh != nil {
		stopCh = machine.stopCh
	}

	graph := cortex.New(cortex.Config{
		InitialNodes:  8,
		PrimeField:    machine.primefield,
		Substrate:     machine.substrate,
		BestFill:      cortex.BestFillFunc(machine.bestFill),
		EigenMode:     machine.eigenMode,
		Sequencer:     machine.sequencer,
		StopCh:        stopCh,
	})

	return graph.Think(prompt, expectedReality)
}

func (machine *Machine) Substrate() *geometry.HybridSubstrate {
	return machine.substrate
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}

type MachineError string

const (
	ErrNoChordFound                MachineError = "no chord found"
	ErrMultiScaleCooccurrenceBuild MachineError = "failed to build multiscale cooccurrence"
)

func (e MachineError) Error() string {
	return string(e)
}
