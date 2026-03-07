package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/vm/cortex"
)

// TestCortexSnapshotCounters verifies that the cortex graph tracks
// observability counters (replacing the old PolicyTracker test).
func TestCortexSnapshotCounters(t *testing.T) {
	graph := cortex.New(cortex.Config{
		InitialNodes: 4,
		MaxTicks:     32,
		MaxOutput:    8,
	})

	snap := graph.Snapshot()
	require.Equal(t, 0, snap.TotalTicks)
	require.Equal(t, 4, snap.FinalNodes)
	require.Equal(t, 0, snap.BedrockQueries)
	require.Equal(t, 0, snap.MitosisEvents)
	require.Equal(t, 0, snap.PruneEvents)
	require.Equal(t, 0, snap.OutputBytes)
}
