package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMachineWithBranchPolicyComposesPolicyTracker(t *testing.T) {
	machine := NewMachine(MachineWithBranchPolicy(BranchPolicy{
		Enabled:         true,
		MarginThreshold: 0.05,
		MaxRetained:     0,
	}))

	retained := 0
	machine.policy.TrackMargin(0.01, &retained)
	machine.policy.TrackMargin(0.02, &retained)
	machine.policy.TrackAnchorVeto()

	snapshot := machine.Observability()
	require.Equal(t, uint64(2), snapshot.LowMarginEvents)
	require.Equal(t, uint64(1), snapshot.RetainedBranches)
	require.Equal(t, uint64(1), snapshot.AnchorVetoEvents)
	require.Equal(t, 1, retained)
}
