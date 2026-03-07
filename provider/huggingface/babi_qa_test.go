package huggingface

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBabiQASamples(t *testing.T) {
	t.Parallel()

	samples := buildBabiQASamples(
		[]string{
			"Mary moved to the bathroom.",
			"John went to the hallway.",
			"Where is Mary?",
			"Daniel went back to the office.",
			"Where is Daniel?",
		},
		[]string{"bathroom", "office"},
		[]int{0, 0, 1, 0, 1},
	)

	require.Len(t, samples, 2)
	require.Equal(t,
		"Mary moved to the bathroom. John went to the hallway. Where is Mary?",
		samples[0].Visible,
	)
	require.Equal(t, "bathroom", samples[0].Answer)
	require.Equal(t,
		"Mary moved to the bathroom. John went to the hallway. Where is Mary? bathroom",
		samples[0].Full,
	)

	require.Equal(t,
		"Mary moved to the bathroom. John went to the hallway. Daniel went back to the office. Where is Daniel?",
		samples[1].Visible,
	)
	require.Equal(t, "office", samples[1].Answer)
}

func TestBuildBabiQASamplesFallsBackToQuestionMarks(t *testing.T) {
	t.Parallel()

	samples := buildBabiQASamples(
		[]string{
			"Mary moved to the bathroom.",
			"Where is Mary?",
		},
		[]string{"bathroom"},
		nil,
	)

	require.Len(t, samples, 1)
	require.Equal(t, "Mary moved to the bathroom. Where is Mary?", samples[0].Visible)
	require.Equal(t, "bathroom", samples[0].Answer)
}
