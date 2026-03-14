package synthesis

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/theapemachine/six/pkg/numeric"
)

/*
FrustrationEngine represents the "Phase-Locked Loop" logic solver.
It acts when a raw sequence fails to span a gap, causing Phase Tension (Frustration).
The Engine vibrates the MacroIndex, applying discovered logic tools until the tension zeros out.
*/
type FrustrationEngine struct {
	calc  *numeric.Calculus
	index *MacroIndex
	rng   *rand.Rand
}

/*
feOpts configuration for FrustrationEngine.
*/
type feOpts func(*FrustrationEngine)

/*
NewFrustrationEngine instantiates the tension-relieving logic solver.
*/
func NewFrustrationEngine(opts ...feOpts) *FrustrationEngine {
	fe := &FrustrationEngine{
		calc:  numeric.NewCalculus(),
		index: NewMacroIndex(),
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, opt := range opts {
		opt(fe)
	}

	return fe
}

/*
WithSharedIndex allows the Frustration Engine to pull from a global Library of OpCodes.
*/
func WithSharedIndex(index *MacroIndex) feOpts {
	return func(fe *FrustrationEngine) {
		fe.index = index
	}
}

/*
WithSeed sets a deterministic random seed for the tool traversal (for testing).
*/
func WithSeed(seed int64) feOpts {
	return func(fe *FrustrationEngine) {
		fe.rng = rand.New(rand.NewSource(seed))
	}
}

/*
Resolve evaluates the frustration (Phase Delta) between reality and belief.
If they don't match, it searches the MacroIndex for a sequential combination of tools
that zeroes the frustration. Returns the tool sequence to jump the span.
*/
func (fe *FrustrationEngine) Resolve(
	currentPhase numeric.Phase,
	targetPhase numeric.Phase,
	maxAttempts int,
) ([]*MacroOpcode, error) {
	if currentPhase == targetPhase {
		// Zero frustration. Already locked.
		return nil, nil
	}

	if currentPhase == 0 || targetPhase == 0 {
		return nil, fmt.Errorf("phase cannot be zero")
	}

	// 1. Direct Resolution check (Cantilever)
	// If a single tool can bridge this gap exactly, use it.
	cl := NewCantilever(currentPhase, targetPhase, WithMacroIndex(fe.index))
	rot, singleTool, err := cl.Bridge()
	
	if err == nil && singleTool.Hardened {
		// A known hardened tool directly solves it.
		return []*MacroOpcode{singleTool}, nil
	}

	// Calculate the delta (frustration scalar for sorting/heuristics if we wanted)
	// Here, we just care if tension != 0 (i.e., state != target)
	
	// Fast path: get all hardened tools available to build a bridge
	tools := fe.index.AvailableHardened()
	if len(tools) == 0 {
		return nil, fmt.Errorf("no hardened tools available in library to relieve frustration gap")
	}

	// 2. Sequential "Vibration" (Random Walk Composition)
	// Try random combination paths of tools until we hit target resonance
	for attempt := 0; attempt < maxAttempts; attempt++ {
		state := currentPhase
		var path []*MacroOpcode

		// Try to bridge using a sequence of 1 to 3 tools
		numTools := fe.rng.Intn(3) + 1
		for step := 0; step < numTools; step++ {
			// Pick a tool
			idx := fe.rng.Intn(len(tools))
			tool := tools[idx]
			
			// Apply tool -- applying the logic circuit rotation (the scalar phase shift)
			state = fe.calc.Multiply(state, tool.Rotation)
			path = append(path, tool)

			if state == targetPhase {
				// Tension Zeroed! We discovered a composed logic circuit.
				// Package this sequence into the single needed rotation and record it.
				fe.index.RecordOpcode(rot)
				return path, nil
			}
		}
	}

	// Tension remains.
	return nil, fmt.Errorf("frustration engine failed to achieve phase-lock after %d attempts", maxAttempts)
}
