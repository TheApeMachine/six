package vm

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/console"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Simplifies generation loops using Toroidal Eigenmodes
and 5-plane Parallel MultiChord searches.
*/
type Machine struct {
	loader     *Loader
	primefield *store.PrimeField
	stopCh     chan struct{}
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
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

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(
	prompt []data.Chord,
	expectedReality *geometry.IcosahedralManifold,
) chan byte {
	out := make(chan byte)

	go func() {
		defer close(out)

		if machine.primefield.N == 0 {
			return
		}

		chords := make([]data.Chord, len(prompt))
		copy(chords, prompt)

		// Spin-Up Phase: Process prompt to build angular momentum
		var zNext uint32
		var byteVal byte

		for _, chord := range chords {
			if key := machine.loader.Store().ReverseLookup(chord); key > 0 {
				_, _, byteVal = tokenizer.NewMortonCoder().Decode(key)
				out <- byteVal
			}

			// Feed the prompt through the Sequencer to build momentum context
			reset, _ := machine.loader.tokenizer.Sequencer().Analyze(int(zNext), chord)

			if reset {
				zNext = 0
			} else {
				zNext++
			}
		}

		// Initial State tracking for geodesic trajectory
		momentum, lastEvents := machine.primefield.Momentum()
		_, phiPhaseThresh := machine.loader.tokenizer.Sequencer().Phase()
		phiDecay := machine.loader.tokenizer.Sequencer().Phi()

		var expRealPtr unsafe.Pointer
		if expectedReality != nil {
			expRealPtr = unsafe.Pointer(expectedReality)
		}

		// Freewheel Phase: Predict forward using momentum
		// Calculate starting topological offset from the ingested prompt
		startIdx := len(chords)
		zNext = uint32(startIdx)

		for range 256 {
			// Natural EOS: generation stops when kinetic rotational energy dissipates
			if momentum < phiPhaseThresh {
				break
			}

			// Apply geodesic extrapolation: Move the mathematical query context forward
			// based on the trajectory defined by the last causal topological events
			var queryCtx geometry.IcosahedralManifold
			for _, ev := range lastEvents {
				// Apply transition matrix to step the state machine forward along the geodesic
				for c := range 5 {
					currentRotState := queryCtx.Header.RotState()
					nextRotState := geometry.StateTransitionMatrix[currentRotState][ev]
					queryCtx.Header.SetRotState(nextRotState)

					// Apply local topological rotation
					switch ev {
					case geometry.EventDensitySpike:
						queryCtx.Cubes[c].RotateX()
					case geometry.EventPhaseInversion:
						queryCtx.Cubes[c].RotateY()
					case geometry.EventDensityTrough:
						queryCtx.Cubes[c].RotateZ()
					case geometry.EventLowVarianceFlux:
						queryCtx.Cubes[c].RotateX()
						queryCtx.Cubes[c].RotateX()
					}
				}
			}

			// Momentum Decay: Physics-based structural friction
			momentum *= phiDecay

			// Broadcast the predicted coordinate to GPU BestFill inference
			// to find the exact historical block that occupies this extrapolated space
			bestIdx, _, err := kernel.BestFill(
				machine.primefield.Field(),
				machine.primefield.N, // Still searches the entire accumulated Field
				unsafe.Pointer(&queryCtx),
				expRealPtr,
				0,
				unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
			)

			if err != nil {
				fmt.Println("BESTFILL ERROR:", err)
				break
			}

			console.Trace("BestFill Retrieved Geodesic Target", "bestIdx", bestIdx)
			matched := machine.primefield.Manifold(bestIdx)

			// Resolve the exact geometric coordinate we are attempting to fill.
			// The stream intrinsically routes linearly through the 5x27 structure
			cubeIndex := int(zNext) % 5
			blockIndex := int(zNext) % 27

			nextChord := matched.Cubes[cubeIndex][blockIndex]

			// Diagnostics: Does the extracted chord contain mass?
			fmt.Printf("Z: %d | C: %d | B: %d | Idx: %d | Active: %d\n", zNext, cubeIndex, blockIndex, bestIdx, nextChord.ActiveCount())

			if nextChord.ActiveCount() == 0 {
				break
			}

			// Translate generated HDC geometric pattern back into standard byte byte
			if key := machine.loader.Store().ReverseLookup(nextChord); key > 0 {
				_, _, b := tokenizer.NewMortonCoder().Decode(key)
				console.Trace("Decoded token byte", "byte", string(b), "key", key)
				out <- b
			}

			// Advance positional sequencing exactly as ingestion did
			reset, evs := machine.loader.tokenizer.Sequencer().Analyze(int(zNext), nextChord)
			if reset {
				zNext = 0
				lastEvents = evs
			} else {
				zNext++
			}
		}
	}()

	return out
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

type MachineError string

const (
	ErrNoChordFound                MachineError = "no chord found"
	ErrMultiScaleCooccurrenceBuild MachineError = "failed to build multiscale cooccurrence"
)

func (e MachineError) Error() string {
	return string(e)
}
