package textgen

import (
	"testing"
)

// TestSpanSolverSuite runs the full BVP text generation experiment sweep.
func TestSpanSolverSuite(t *testing.T) {
	exp := New()

	err := exp.Run()
	if err != nil {
		t.Fatalf("BVP Span Solver Experiment failed: %v", err)
	}
}
