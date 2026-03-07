package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/tokenizer"
)

func TestLanguagesExperimentHoldoutUsesRightSideAsTarget(t *testing.T) {
	t.Parallel()

	experiment := NewLanguagesExperiment()
	pct, holdoutType := experiment.Holdout()

	require.Equal(t, 50, pct)
	require.Equal(t, tokenizer.RIGHT, holdoutType)
}
