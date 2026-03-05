package vm

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
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
	stopCh     chan struct{}
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
	machine.stopCh = make(chan struct{})
	var chords []data.Chord

	for chord := range machine.loader.Generate() {
		machine.primefield.Insert(chord)
		chords = append(chords, chord)
	}
	
	fmt.Println("Start inserted chords:", len(chords)) // Debug!

	if err := machine.eigen.BuildMultiScaleCooccurrence(chords); err != nil {
		return console.Error(fmt.Errorf("failed to build multiscale cooccurrence: %w", err),
			"total_chords", len(chords),
			"store", machine.loader.holdoutType,
		)
	}

	// Start asynchronous continuous metabolic consolidation
	if machine.loader != nil && machine.loader.Store() != nil {
		go machine.loader.Store().SleepCycle(machine.stopCh)
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
SpanResult is the output of a single GPU MultiChord probe.
*/
type SpanResult struct {
	Index int
	Score float64
	Chord geometry.IcosahedralManifold
}

/*
Prompt simply clamps the input, executes a parallel GPU BestFill over all Fibonacci
planes simultaneously, checks Eigenmode Intent alignment, and loops until
the structure collapses or hits an end-token.
*/
func (machine *Machine) Prompt(prompt []data.Chord, expectedReality *geometry.IcosahedralManifold) chan SpanResult {
	out := make(chan SpanResult)

	go func() {
		defer close(out)

		// Track masked indices so we can unmask them when done
		var masked []struct {
			idx   int
			chord geometry.IcosahedralManifold
		}

		defer func() {
			for _, m := range masked {
				machine.primefield.Unmask(m.idx, m.chord)
			}
		}()

		startIdx := machine.primefield.N - len(prompt)
		if startIdx < 0 {
			startIdx = 0
		}
		currentIdx := startIdx

		for _ = range prompt {
			if currentIdx < machine.primefield.N {
				masked = append(masked, struct {
					idx   int
					chord geometry.IcosahedralManifold
				}{currentIdx, machine.primefield.Mask(currentIdx)})
				currentIdx++
			}
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
			// Build current active context Manifold directly matching GPU topology
			var activeCtx geometry.IcosahedralManifold
			if len(prompt[start:]) > 0 {
				tempField := store.NewPrimeField()
				for _, c := range prompt[start:] {
					tempField.Insert(c)
				}
				activeCtx = tempField.Manifold(tempField.N - 1)
			}

			// Prepare ExpectedReality pointer
			var expectedPtr unsafe.Pointer
			if expectedReality != nil {
				expectedPtr = unsafe.Pointer(expectedReality)
			} else {
				expectedPtr = unsafe.Pointer(&activeCtx) // Fallback to normal context matching
			}

			dictPtr, numChords := machine.primefield.Snapshot()
			
			// GPU Bitwise Search - System 1 (Fast, Discrete)
			bestIdx, score, err := kernel.BestFill(
				dictPtr,
				numChords,
				unsafe.Pointer(&activeCtx),
				expectedPtr,
				currentIdx,
				unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
			)

			// Ambiguity Check & Hybrid Routing (System 2)
			if err == nil && bestIdx >= 0 && machine.eigen != nil {
				// Temporarily zero the best result 
				originalRoot := machine.primefield.Mask(bestIdx)
				
				// Re-run GPU search to find the second-best competitor
				altDictPtr, altNumChords := machine.primefield.Snapshot()
				altIdx, altScore, _ := kernel.BestFill(
					altDictPtr,
					altNumChords,
					unsafe.Pointer(&activeCtx),
					expectedPtr,
					currentIdx,
					unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)
				
				machine.primefield.Unmask(bestIdx, originalRoot)
				
				// If the score differential is extremely tight, we have logical contradiction
				// or semantic ambiguity. Route to System 2 (EigenMode) for slow, continuous resolution.
				if (score - altScore) < 0.05 {
					// 1. Establish the "Anchor Phase" of our current context
					ctxTheta, ctxPhi := machine.eigen.SeqToroidalMeanPhase(prompt[start:])
					
					// 2. Fetch the discrete candidate manifolds
					cand1 := machine.primefield.Manifold(bestIdx).Cubes[0][0]
					cand2 := machine.primefield.Manifold(altIdx).Cubes[0][0]
					
					// 3. Extrapolate candidate continuous phases
					c1Theta, c1Phi := machine.eigen.PhaseForChord(&cand1)
					c2Theta, c2Phi := machine.eigen.PhaseForChord(&cand2)
					
					// 4. Calculate toroidal L2 distance inside S1 x S1
					dist1 := math.Sqrt(math.Pow(c1Theta-ctxTheta, 2) + math.Pow(c1Phi-ctxPhi, 2))
					dist2 := math.Sqrt(math.Pow(c2Theta-ctxTheta, 2) + math.Pow(c2Phi-ctxPhi, 2))
					
					// 5. System 2 Override: If candidate 2 is topologically closer, override System 1
					if dist2 < dist1 {
						bestIdx = altIdx
						score = altScore
					}
				}
			}

			found := machine.primefield.Manifold(bestIdx)

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
				if bestIdx+i >= machine.primefield.N {
					break
				}
				original := machine.primefield.Mask(bestIdx + i)

				masked = append(masked, struct {
					idx   int
					chord geometry.IcosahedralManifold
				}{bestIdx + i, original})
			}

			// We only care about the origin origin block [0][0] for immediate sequence tokenization
			foundChord := found.Cubes[0][0]
			activeChord := activeCtx.Cubes[0][0]
			
			// If our current prompt perfectly overlaps the bedrock chord, it means 
			// the wave completely canceled out (0 entropy hole). To continue the generation,
			// we must step the read head forward by 1 index to grab the "next" token!
			missingChord := data.ChordHole(&foundChord, &activeChord)
			
			if missingChord.ActiveCount() == 0 {
				// Perfect match up to this point! The missing piece is literally the NEXT token
				// inside the found bedrock geometry. So we advance the index.
				if bestIdx+len(prompt) < machine.primefield.N {
					nextManifold := machine.primefield.Manifold(bestIdx + len(prompt))
					missingChord = nextManifold.Cubes[0][0]
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
				manifold := machine.primefield.Manifold(startIndex + offset)

				out <- SpanResult{
					Index: startIndex + offset,
					Score: score,
					Chord: manifold,
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