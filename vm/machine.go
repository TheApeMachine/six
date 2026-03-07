package vm

import (
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/vm/cortex"
	"github.com/theapemachine/six/vm/generation"
)

// maxGenerationSteps is the maximum number of tokens to generate in a single prompt.
const maxGenerationSteps = 256
const maxReasoningHops = 3

type bestFillFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

type bestFillWithFieldFn func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedField *geometry.ExpectedField,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

type BranchPolicy struct {
	Enabled         bool
	MarginThreshold float64
	MaxRetained     int
}

type ObservabilitySnapshot struct {
	LowMarginEvents  uint64
	RetainedBranches uint64
	AnchorVetoEvents uint64
}

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Simplifies generation loops using Toroidal Eigenmodes
and 5-plane Parallel MultiChord searches.
*/
type Machine struct {
	loader            *Loader
	primefield        *store.PrimeField
	bestFill          bestFillFn
	bestFillWithField bestFillWithFieldFn
	policy            *generation.PolicyTracker
	stopCh            chan struct{}
	useCortex         bool
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		bestFill:   kernel.BestFill,
		policy:     generation.NewPolicyTracker(generation.DefaultBranchPolicy()),
		bestFillWithField: func(
			dictionary unsafe.Pointer,
			numChords int,
			context unsafe.Pointer,
			expectedReality unsafe.Pointer,
			expectedField *geometry.ExpectedField,
			_ int,
			geodesicLUT unsafe.Pointer,
		) (int, float64, error) {
			return kernel.BestFillWithExpectedField(
				dictionary,
				numChords,
				context,
				expectedReality,
				expectedField,
				geodesicLUT,
			)
		},
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

func (machine *Machine) Start() error {
	machine.stopCh = make(chan struct{})

	for range machine.loader.Generate() {
		// Loader now intrinsically pipes topological sequences into the PrimeField
	}

	return nil
}

/*
Stop terminates the Machine and signaling any background processes to finish.
*/
func (machine *Machine) Stop() {
	if machine.stopCh != nil {
		close(machine.stopCh)
		machine.stopCh = nil
	}
}

func (machine *Machine) PromptWithExpectedField(
	prompt []data.Chord,
	expectedField *geometry.ExpectedField,
) chan byte {
	return machine.promptInternal(prompt, geometry.ExpectedManifoldFromField(expectedField), expectedField)
}

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	return machine.promptInternal(prompt, expectedReality, nil)
}

func (machine *Machine) promptInternal(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
	expectedField *geometry.ExpectedField,
) chan byte {
	return machine.promptCortex(prompt, expectedReality)
}

// promptCortex spawns a volatile cortex graph that vibrates until convergence,
// representing the core spatial inference process.
func (machine *Machine) promptCortex(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	console.Info("machine: starting generation with cortex model",
		"promptLen", len(prompt),
	)

	graph := cortex.New(cortex.Config{
		InitialNodes: 8,
		PrimeField:   machine.primefield,
		BestFill:     cortex.BestFillFunc(machine.bestFill),
		MaxTicks:     maxGenerationSteps * 4, // budget: 4 ticks per output byte
		MaxOutput:    maxGenerationSteps,
	})

	return graph.Think(prompt, expectedReality)
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}

func MachineWithPrimeField(pf *store.PrimeField) machineOpts {
	return func(machine *Machine) {
		machine.primefield = pf
	}
}

func MachineWithBestFill(fn bestFillFn) machineOpts {
	return func(machine *Machine) {
		if fn != nil {
			machine.bestFill = fn
			machine.bestFillWithField = func(
				dictionary unsafe.Pointer,
				numChords int,
				context unsafe.Pointer,
				expectedReality unsafe.Pointer,
				_ *geometry.ExpectedField,
				mode int,
				geodesicLUT unsafe.Pointer,
			) (int, float64, error) {
				return fn(dictionary, numChords, context, expectedReality, mode, geodesicLUT)
			}
		}
	}
}

func MachineWithBranchPolicy(policy BranchPolicy) machineOpts {
	return func(machine *Machine) {
		if machine.policy == nil {
			machine.policy = generation.NewPolicyTracker(generation.DefaultBranchPolicy())
		}
		machine.policy.SetPolicy(generation.BranchPolicy{
			Enabled:         policy.Enabled,
			MarginThreshold: policy.MarginThreshold,
			MaxRetained:     policy.MaxRetained,
		})
	}
}

// MachineWithCortex enables the reactive working-memory cortex.
// When active, prompts spawn a volatile graph of resonating MacroCubes
// instead of the linear Runner. The old Runner path remains accessible
// when cortex mode is not enabled.
func MachineWithCortex() machineOpts {
	return func(machine *Machine) {
		machine.useCortex = true
	}
}

func (machine *Machine) Observability() ObservabilitySnapshot {
	if machine.policy == nil {
		return ObservabilitySnapshot{}
	}

	snapshot := machine.policy.Snapshot()

	return ObservabilitySnapshot{
		LowMarginEvents:  snapshot.LowMarginEvents,
		RetainedBranches: snapshot.RetainedBranches,
		AnchorVetoEvents: snapshot.AnchorVetoEvents,
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
