package vm

import (
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/gpu/metal"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/store"
)

const (
	// snapThreshold is the minimum resonance for a span to hold.
	snapThreshold = 0.85
	// topK is the number of alternate candidates held per probe.
	topK = 5
)

/*
Machine is the entrypoint to the architecture.
It loads the initial data into the store and is then ready for
prompting. Prompting is essentially the same thing as loading data,
and can follow the same mechanism, it just comes with some extra steps,
one of which is to generate an actual output.
*/
type Machine struct {
	loader *Loader
	primefield *store.PrimeField
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

func (machine *Machine) Start() {
	for token := range machine.loader.Generate() {
		machine.primefield.Insert(token.Chord, token.TokenID)
	}
}

/*
SpanResult is the output of a single cantilever probe.
*/
type SpanResult struct {
	Index int
	Key   uint64
	Scale int
	Score float64
	Chord data.Chord
}

/*
Prompt runs the full cantilever-based span retrieval with pathfinding.

For each prompt token from the loader:
 1. Build the context chord from the prompt window.
 2. Try FibWindows from largest (21) to smallest (3).
 3. For each scale, GPU scans the PrimeField via BestFill.
 4. If resonance >= snapThreshold → span holds, emit with overlap.
 5. If all scales snap → emit the best-scoring candidate at the
    smallest scale (never get stuck).
 6. Advance by overlap (scale * 0.6) to create overlapping joints
    that act as structural checksums between spans.
 7. If the next probe returns terrible scores across all scales,
    backtrack: pop the last emitted span, try the next-best
    candidate from the saved top-K alternatives.
*/
type spanFrame struct {
	result       SpanResult
	alternatives []SpanResult
	depth        int
}

/*
Prompt runs the full cantilever-based span retrieval with pathfinding.
*/
func (machine *Machine) Prompt(prompt []data.Chord) chan SpanResult {
	out := make(chan SpanResult)

	go func() {
		defer close(out)

		// Create a sliding context buffer of maximum Fibonacci window size.
		maxScale := numeric.FibWindows[0]
		contextBuf := make([]data.Chord, 0, maxScale)

		// 1. Fill the initial context from the prompt.
		for _, chord := range prompt {
			contextBuf = append(contextBuf, chord)
			if len(contextBuf) > maxScale {
				contextBuf = contextBuf[1:]
			}
			// Emit prompt tokens as baseline results.
			out <- SpanResult{
				Chord: chord,
				Scale: 1,
				Score: 1.0,
			}
		}

		if len(contextBuf) == 0 {
			return
		}

		// The generated output as a stack for backtracking.
		var stack []spanFrame
		var depth int

		// We assume generation continues indefinitely or until max depth.
		// For the sake of this prompt, we generate continuously.
		for {
			result, alternatives := machine.probe(contextBuf)

			// Check if we need to backtrack.
			if result.Score < snapThreshold && len(stack) > 0 {
				// Current path is a dead end. Pop the last span and try an alternative.
				prev := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				rewound := false
				for i, alt := range prev.alternatives {
					if alt.Score >= snapThreshold*0.9 {
						// Remove this alternative so we don't try it again if we backtrack to here
						var remainingAlts []SpanResult
						if i+1 < len(prev.alternatives) {
							remainingAlts = prev.alternatives[i+1:]
						}

						stack = append(stack, spanFrame{
							result:       alt,
							alternatives: remainingAlts,
							depth:        prev.depth,
						})
						
						// Restore contextBuf to the state it was before the popped span.
						// This is complex in a streaming buffer, so a simpler backtracking
						// approach is to rebuild the context from the stack history.
						contextBuf = machine.rebuildContext(stack, maxScale)
						
						// Backtracking rewind
						rewound = true
						
						advance := max(int(float64(alt.Scale) * 0.6), 1)

						for i := 0; i < advance; i++ {
							idx := alt.Index + i
							if idx < machine.primefield.N {
								out <- SpanResult{
									Index: idx,
									Key:   machine.primefield.Key(idx),
									Scale: alt.Scale,
									Score: alt.Score,
									Chord: machine.primefield.Chord(idx),
								}
							}
						}
						break
					}
				}

				if rewound {
					continue
				}

				// No viable alternatives — accept the best we had, and we are forced to proceed.
				stack = append(stack, prev)
				contextBuf = machine.rebuildContext(stack, maxScale)
			} else {
				// Emit the result and push to stack.
				stack = append(stack, spanFrame{
					result:       result,
					alternatives: alternatives,
					depth:        depth,
				})

				advance := int(float64(result.Scale) * 0.6)
				if advance < 1 {
					advance = 1
				}

				for i := 0; i < advance; i++ {
					idx := result.Index + i
					if idx < machine.primefield.N {
						out <- SpanResult{
							Index: idx,
							Key:   machine.primefield.Key(idx),
							Scale: result.Scale,
							Score: result.Score,
							Chord: machine.primefield.Chord(idx),
						}
					}
				}
				depth++
			}

			// Replace contextBuf generation to actually advance structurally the overlapping chunk
			advance := int(float64(result.Scale) * 0.6)
			if advance < 1 {
				advance = 1
			}

			// We need the sequence from the corpus shifted forward by `advance` bytes.
			// Since primefield tokens of the same scale are sequentially stored matching the corpus position:
			nextIdx := result.Index + advance
			var nextContext data.Chord
			if nextIdx < machine.primefield.N {
				nextContext = machine.primefield.Chord(nextIdx)
			} else {
				nextContext = result.Chord
			}

			contextBuf = append(contextBuf, nextContext)
			if len(contextBuf) > maxScale {
				contextBuf = contextBuf[1:]
			}
		}
	}()

	return out
}

func (machine *Machine) rebuildContext(stack []spanFrame, maxScale int) []data.Chord {
	var buf []data.Chord
	for _, frame := range stack {
		buf = append(buf, frame.result.Chord)
	}
	if len(buf) > maxScale {
		buf = buf[len(buf)-maxScale:]
	}
	return buf
}

/*
probe runs the cantilever: tries FibWindows from largest to smallest,
returns the best result and up to topK alternatives for backtracking.
Uses a multi-pass Mask/Unmask strategy to get Top-K from the GPU.
*/
func (machine *Machine) probe(contextBuf []data.Chord) (SpanResult, []SpanResult) {
	var best SpanResult
	var alternatives []SpanResult

	// Walk FibWindows in reverse: 21, 13, 8, 5, 3
	for i := len(numeric.FibWindows) - 1; i >= 0; i-- {
		scale := numeric.FibWindows[i]
		if len(contextBuf) < scale {
			continue // Context not large enough for this window yet
		}

		// The single recently advanced aggregate chord is the next BVP context query
		latestChord := contextBuf[len(contextBuf)-1]
		topKResults := machine.probeTopK([]data.Chord{latestChord}, scale, topK)

		if len(topKResults) == 0 {
			continue
		}

		result := topKResults[0]
		alts := topKResults[1:]

		// The cantilever holds — use this scale.
		if result.Score >= snapThreshold {
			return result, alts
		}

		// Track the best across all scales as fallback.
		if result.Score > best.Score {
			best = result
			alternatives = alts
		}
	}

	return best, alternatives
}

// probeTopK executes a multi-pass BestFill to find the top K matches.
func (machine *Machine) probeTopK(buf []data.Chord, scale int, k int) []SpanResult {
	var results []SpanResult
	
	// Keep track of original masked chords to restore them later.
	maskedIdxs := make([]int, 0, k)
	maskedChords := make([]data.Chord, 0, k)
	
	defer func() {
		// Always unmask all masked chords.
		for i, idx := range maskedIdxs {
			machine.primefield.Unmask(idx, maskedChords[i])
		}
	}()

	for range k {
		bestIdx, score, err := metal.BestFill(
			machine.primefield.Field(),
			machine.primefield.N,
			unsafe.Pointer(&buf[0]),
		)

		if err != nil || bestIdx < 0 || bestIdx >= machine.primefield.N {
			break
		}

		chord := machine.primefield.Chord(bestIdx)
		
		results = append(results, SpanResult{
			Index: bestIdx,
			Key:   machine.primefield.Key(bestIdx),
			Scale: scale,
			Score: score,
			Chord: chord,
		})

		// Mask this best result to find the next best in the next pass.
		orig := machine.primefield.Mask(bestIdx)
		maskedIdxs = append(maskedIdxs, bestIdx)
		maskedChords = append(maskedChords, orig)
	}

	return results
}

func MachineWithLoader(loader *Loader) machineOpts {
	return func(machine *Machine) {
		machine.loader = loader
	}
}