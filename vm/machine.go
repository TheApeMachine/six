package vm

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/gpu/metal"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/store"
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
	eigen      *geometry.EigenMode
}

type machineOpts func(*Machine)

/*
NewMachine creates a new Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		primefield: store.NewPrimeField(),
		eigen:      geometry.NewEigenMode(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	return machine
}

func (machine *Machine) Start() error {
	var chords []data.Chord

	for chord := range machine.loader.Generate() {
		machine.primefield.Insert(chord)
		chords = append(chords, chord)
	}
	if err := machine.eigen.BuildMultiScaleCooccurrence(chords); err != nil {
		return console.Error(fmt.Errorf("failed to build multiscale cooccurrence: %w", err),
			"total_chords", len(chords),
			"store", machine.loader.holdoutType,
		)
	}
	return nil
}

/*
SpanResult is the output of a single GPU MultiChord probe.
*/
type SpanResult struct {
	Index int
	Score float64
	Chord data.MultiChord
}

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(prompt []data.Chord) chan SpanResult {
	out := make(chan SpanResult)

	go func() {
		defer close(out)

		// Track masked indices so we can unmask them when done
		var masked []struct {
			idx   int
			chord data.MultiChord
		}

		defer func() {
			for _, m := range masked {
				machine.primefield.Unmask(m.idx, m.chord)
			}
		}()

		currentIdx := 0

		for i := range prompt {
			currentIdx += i
			masked = append(masked, struct {
				idx   int
				chord data.MultiChord
			}{currentIdx, machine.primefield.Mask(currentIdx)})
		}

		for {
			// Build current active context MultiChord directly matching GPU topology
			var activeCtx data.MultiChord

			for i := range numeric.FibWindows {
				var agg data.Chord
				
				for _, c := range prompt {
					for j := range agg {
						agg[j] = c[j]
					}
				}
				
				activeCtx[i] = agg
			}

			// GPU Bitwise Search (all 5 spatial planes instantly!)
			bestIdx, score, err := metal.BestFill(
				machine.primefield.Field(),
				machine.primefield.N,
				unsafe.Pointer(&activeCtx),
				currentIdx,
			)

			found := machine.primefield.MultiChord(bestIdx)

			if err != nil || bestIdx < 0 || bestIdx >= machine.primefield.N {
				console.Error(MachineErrNotFound,
					"error", err,
					"bestIdx", bestIdx,
					"fieldN", machine.primefield.N,
					"currentIdx", bestIdx,
				)
				return
			}
			
			// Mask the found index — zero it in the PrimeField so the 
			// GPU can't re-find it. Store original for deferred unmask.
			for i := range prompt {
				original := machine.primefield.Mask(bestIdx)

				masked = append(masked, struct {
					idx   int
					chord data.MultiChord
				}{bestIdx+i, original})
			}

			out <- SpanResult{
				Index: bestIdx,
				Score: score,
				Chord: found,
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

func MachineWithEigenMode(eigen *geometry.EigenMode) machineOpts {
	return func(machine *Machine) {
		machine.eigen = eigen
	}
}

type MachineError string

const (
	MachineErrNotFound MachineError = "no chord found"
)

func (e MachineError) Error() string {
	return string(e)
}