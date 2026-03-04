package vm

import (
	"math"
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
	
	return machine.eigen.BuildMultiScaleCooccurrence(chords)
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

		buf := append([]data.Chord{}, prompt...)
		currentIdx := 0

		for {
			if len(buf) == 0 {
				return
			}

			// Build current active context MultiChord directly matching GPU topology
			var activeCtx data.MultiChord

			for i, w := range numeric.FibWindows {
				start := max(len(buf) - w, 0)
				
				var agg data.Chord
				
				for _, c := range buf[start:] {
					for j := range agg {
						agg[j] |= c[j]
					}
				}
				
				activeCtx[i] = agg
			}

			// Context toroidal phase from chord sequence (chord-native)
			ctxTheta, ctxPhi := machine.eigen.SeqToroidalMeanPhase(buf)

			// GPU Bitwise Search (all 5 spatial planes instantly!)
			bestIdx, score, err := metal.BestFill(
				machine.primefield.Field(),
				machine.primefield.N,
				unsafe.Pointer(&activeCtx),
				currentIdx,
			)

			if err != nil || bestIdx < 0 || bestIdx >= machine.primefield.N {
				console.Error(MachineErrNotFound)
				return
			}

			mChord := machine.primefield.MultiChord(bestIdx)

			// Semantic compass: phase from candidate chord (finest scale)
			candChord := mChord[0]
			candTheta, candPhi := machine.eigen.PhaseForChord(&candChord)

			diffTheta := math.Abs(candTheta - ctxTheta)
			
			if diffTheta > math.Pi {
				diffTheta = 2*math.Pi - diffTheta
			}

			diffPhi := math.Abs(candPhi - ctxPhi)
			
			if diffPhi > math.Pi {
				diffPhi = 2*math.Pi - diffPhi
			}

			// Deduct resonance percentage points for Toroidal misalignment
			// NOTE: This should be a derived, not a guessed value.
			score -= (diffTheta / math.Pi) * 0.10
			score -= (diffPhi / math.Pi) * 0.10

			out <- SpanResult{
				Index: bestIdx,
				Score: score,
				Chord: mChord,
			}

			// Append candidate chord to sliding window (chord-native)
			buf = append(buf, candChord)

			if len(buf) > 21 {
				buf = buf[1:]
			}

			currentIdx = bestIdx + 1 // Advance 
			// (NOTE: Should this not take into account the span length?)
		}
	}()

	return out
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}

type MachineError string

const (
	MachineErrNotFound MachineError = "no chord found"
)

func (e MachineError) Error() string {
	return string(e)
}