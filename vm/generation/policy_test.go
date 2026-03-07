package generation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyTrackerMarginDisabled(t *testing.T) {
	tracker := NewPolicyTracker(BranchPolicy{Enabled: false, MarginThreshold: 0.05, MaxRetained: 2})

	retained := 0
	tracker.TrackMargin(0.01, &retained)

	snapshot := tracker.Snapshot()
	require.Equal(t, uint64(0), snapshot.LowMarginEvents)
	require.Equal(t, uint64(0), snapshot.RetainedBranches)
	require.Equal(t, 0, retained)
}

func TestPolicyTrackerMarginEnabledAndCapped(t *testing.T) {
	tracker := NewPolicyTracker(BranchPolicy{Enabled: true, MarginThreshold: 0.05, MaxRetained: 2})

	retained := 0
	for _, margin := range []float64{0.01, 0.02, 0.08, 0.01} {
		tracker.TrackMargin(margin, &retained)
	}

	snapshot := tracker.Snapshot()
	require.Equal(t, uint64(3), snapshot.LowMarginEvents)
	require.Equal(t, uint64(2), snapshot.RetainedBranches)
	require.Equal(t, 2, retained)
}

func TestPolicyTrackerMarginAllowsNilRetainedPointer(t *testing.T) {
	tracker := NewPolicyTracker(BranchPolicy{Enabled: true, MarginThreshold: 0.05, MaxRetained: 2})

	tracker.TrackMargin(0.01, nil)

	snapshot := tracker.Snapshot()
	require.Equal(t, uint64(1), snapshot.LowMarginEvents)
	require.Equal(t, uint64(0), snapshot.RetainedBranches)
}

func TestPolicyTrackerAnchorVetoCounter(t *testing.T) {
	tracker := NewPolicyTracker(DefaultBranchPolicy())

	tracker.TrackAnchorVeto()
	tracker.TrackAnchorVeto()

	snapshot := tracker.Snapshot()
	require.Equal(t, uint64(2), snapshot.AnchorVetoEvents)
}

func TestPolicyTrackerSetPolicyNormalizesMaxRetained(t *testing.T) {
	tracker := NewPolicyTracker(DefaultBranchPolicy())
	tracker.SetPolicy(BranchPolicy{Enabled: true, MarginThreshold: 0.05, MaxRetained: 0})

	policy := tracker.Policy()
	require.Equal(t, 1, policy.MaxRetained)
}
