package synthesis

import (
	"fmt"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
Cantilever is a Boundary Value Problem (BVP) synthesis engine.
It uses phase mismatch between Start and Goal points to drive a "Frustration Engine"
that synthesizes or discovers logical rotation tools that map across the span.
*/
type Cantilever struct {
	calc       *numeric.Calculus
	StartPhase numeric.Phase
	GoalPhase  numeric.Phase
	Index      *MacroIndex
}

/*
opts ...
*/
type opts func(*Cantilever)

/*
NewCantilever provides a new logic solver acting between fixed start and end boundary supports.
*/
func NewCantilever(start, goal numeric.Phase, options ...opts) *Cantilever {
	cl := &Cantilever{
		calc:       numeric.NewCalculus(),
		StartPhase: start,
		GoalPhase:  goal,
		Index:      NewMacroIndex(),
	}

	for _, opt := range options {
		opt(cl)
	}

	return cl
}

/*
WithMacroIndex injects a shared MacroIndex library to utilize discovered
Logic Circuits across Cantilever instances.
*/
func WithMacroIndex(index *MacroIndex) opts {
	return func(cl *Cantilever) {
		cl.Index = index
	}
}

/*
Bridge synthesizes a path between the Start and Goal boundaries.
It computes the Delta Rotation (G^X) necessary to transit the gap directly.
If standard raw navigation fails, it pulls a MacroOpcode or creates one.
*/
func (cl *Cantilever) Bridge() (numeric.Phase, *MacroOpcode, error) {
	if cl.StartPhase == 0 || cl.GoalPhase == 0 {
		return 0, nil, fmt.Errorf("cantilever boundaries cannot be absolute zero")
	}

	if cl.StartPhase == cl.GoalPhase {
		return 0, nil, fmt.Errorf("start and goal phases identical, bridge span length is 0")
	}

	// Calculate the necessary Phase Shift (Tool Rotation) to bridge the gap:
	// Rot = (Goal Phase * Inverse(Start Phase)) % 257
	inverseStart, err := cl.calc.Inverse(cl.StartPhase)
	if err != nil {
		return 0, nil, fmt.Errorf("could not compute inverse for cantilever start boundary: %w", err)
	}
	targetRotation := cl.calc.Multiply(cl.GoalPhase, inverseStart)

	// Step 1: Scan library for a known tool capable of bridging this span
	op, found := cl.Index.FindOpcode(targetRotation)
	if found {
		// Tool found. We successfully span the gap using pre-synthesized logic constraints.
		cl.Index.RecordOpcode(targetRotation) // Increment usage
		return targetRotation, op, nil
	}

	// Step 2: The Cantilever fails via Frustration (no known tool can bridge).
	// We synthesize a generalized patch for this Delta and "Harden" it.
	cl.Index.RecordOpcode(targetRotation) // Synthesize new!
	opNew, _ := cl.Index.FindOpcode(targetRotation)

	return targetRotation, opNew, nil
}
