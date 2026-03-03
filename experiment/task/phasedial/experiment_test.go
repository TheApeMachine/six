package phasedial

import (
	"testing"
)

// TestPhaseDialSuite runs the full PhaseDial geometry validation sweep.
// This is mainly a functional execution test to ensure the mathematical operations
// and metrics calculations do not panic or regress structurally during refactoring.
func TestPhaseDialSuite(t *testing.T) {
	exp := New()
	
	// Execute the full trace, asserting no runtime panics occur.
	// Real geodesic trace output measurements are logged to STDOUT.
	err := exp.Run()
	if err != nil {
		t.Fatalf("PhaseDial Experiment Sweep failed: %v", err)
	}
}
