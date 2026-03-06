package tokenizer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

type promptMockDataset struct {
	samples []string
}

func (dataset promptMockDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)
	go func() {
		defer close(out)
		for sid, sample := range dataset.samples {
			for pos := range len(sample) {
				out <- provider.RawToken{
					SampleID: uint32(sid),
					Symbol:   sample[pos],
					Pos:      uint32(pos),
				}
			}
		}
	}()

	return out
}

func asByte(ch data.Chord, expected []byte) byte {
	for _, candidate := range expected {
		if ch == data.BaseChord(candidate) {
			return candidate
		}
	}

	return 0
}

func TestPromptValueStaysAlignedWithNext(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithDataset(promptMockDataset{samples: []string{"abc", "xyz"}}),
		PromptWithHoldout(50, RIGHT),
	)

	first := prompt.Next()
	require.Len(t, first, 1)
	require.Equal(t, byte('a'), asByte(first[0], []byte{'a', 'x'}))
	require.Equal(t, "bc", prompt.Value(0))

	second := prompt.Next()
	require.Len(t, second, 1)
	require.Equal(t, byte('x'), asByte(second[0], []byte{'a', 'x'}))
	require.Equal(t, "yz", prompt.Value(1))

	require.Equal(t, "bc", prompt.Value(0))
}
