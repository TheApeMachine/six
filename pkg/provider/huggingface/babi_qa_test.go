package huggingface

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/pkg/provider"
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
		"Mary moved to the bathroom. John went to the hallway. Where is Mary?bathroom",
		samples[0].Full,
	)

	require.Equal(t,
		"Mary moved to the bathroom. John went to the hallway. Daniel went back to the office. Where is Daniel?",
		samples[1].Visible,
	)
	require.Equal(t, "office", samples[1].Answer)
	require.Equal(t,
		"Mary moved to the bathroom. John went to the hallway. Daniel went back to the office. Where is Daniel?office",
		samples[1].Full,
	)
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
	require.Equal(t, "Mary moved to the bathroom. Where is Mary?bathroom", samples[0].Full)
}

func TestBabiQAGeneratePreservesSampleContinuity(t *testing.T) {
	t.Parallel()

	dataset := &BabiQADataset{
		samples: []BabiQASample{
			{Full: "A. B?room"},
			{Full: "C. D?hallway"},
		},
	}

	dataset.once.Do(func() {})

	var tokens []provider.RawToken
	for token := range dataset.Generate() {
		tokens = append(tokens, token)
	}

	full0 := []byte(dataset.samples[0].Full)
	full1 := []byte(dataset.samples[1].Full)

	require.Len(t, tokens, len(full0)+len(full1))

	for idx, b := range full0 {
		require.Equal(t, uint32(0), tokens[idx].SampleID)
		require.Equal(t, uint32(idx), tokens[idx].Pos)
		require.Equal(t, b, tokens[idx].Symbol)
	}

	offset := len(full0)
	for idx, b := range full1 {
		require.Equal(t, uint32(1), tokens[offset+idx].SampleID)
		require.Equal(t, uint32(idx), tokens[offset+idx].Pos)
		require.Equal(t, b, tokens[offset+idx].Symbol)
	}
}
