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
func (machine *Machine) Prompt(prompt []data.Chord, expectedReality *data.MultiChord) chan SpanResult {
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


		// We accumulate the context into a single window.
		// As per BITWISE.md, we scan backwards from the end of the prompt until we hit a space boundary
		// or up to a max window size (21, max Fibonacci block size)
		window := 21
		start := len(prompt) - window
		if start < 0 {
			start = 0
		}
		
		for {
			// Build current active context MultiChord directly matching GPU topology
			var activeCtx data.MultiChord

			for i := range numeric.FibWindows {
				var agg data.Chord
				
				for _, c := range prompt[start:] {
					for j := range agg {
						agg[j] |= c[j] // Bitwise OR to accumulate semantic features
					}
				}
				
				activeCtx[i] = agg
			}

			// Prepare ExpectedReality pointer
			var expectedPtr unsafe.Pointer
			if expectedReality != nil {
				expectedPtr = unsafe.Pointer(expectedReality)
			} else {
				expectedPtr = unsafe.Pointer(&activeCtx) // Fallback to normal context matching
			}

			// GPU Bitwise Search (all 5 spatial planes instantly!)
			bestIdx, score, err := metal.BestFill(
				machine.primefield.Field(),
				machine.primefield.N,
				unsafe.Pointer(&activeCtx),
				expectedPtr,
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
				original := machine.primefield.Mask(bestIdx + i)

				masked = append(masked, struct {
					idx   int
					chord data.MultiChord
				}{bestIdx+i, original})
			}

			// We only care about the first layer [0] for immediate sequence tokenization
			foundChord := found[0]
			activeChord := activeCtx[0]
			
			// If our current prompt perfectly overlaps the bedrock chord, it means 
			// the wave completely canceled out (0 entropy hole). To continue the generation,
			// we must step the read head forward by 1 index to grab the "next" token!
			missingChord := data.ChordHole(&foundChord, &activeChord)
			
			if missingChord.ActiveCount() == 0 {
				// Perfect match up to this point! The missing piece is literally the NEXT token
				// inside the found bedrock geometry. So we advance the index.
				if bestIdx+len(prompt) < machine.primefield.N {
					nextChord := machine.primefield.MultiChord(bestIdx + len(prompt))
					missingChord = nextChord[0]
				}
			}

			// The GPU found a topological match (`bestIdx`) for our prompt.
			// Because `bestIdx` points to the *start* of where our active context matched in the 
			// PrimeField historical store, we must advance exactly `len(prompt)` tokens 
			// forward to retrieve the NEW characters that exist AFTER the prompt sequence.
			startIndex := bestIdx
			if missingChord.ActiveCount() == 0 {
				startIndex = bestIdx + len(prompt)
			}
			for offset := 0; offset < 10 && startIndex+offset < machine.primefield.N; offset++ {
				rawChord := machine.primefield.MultiChord(startIndex + offset)[0]
				var singleChord data.MultiChord
				singleChord[0] = rawChord // We package it in the format the decoder expects

				out <- SpanResult{
					Index: startIndex + offset,
					Score: score,
					Chord: singleChord,
				}
			}
			
			// We only want the sequence corresponding to the very first prompt completion chunk.
			break
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