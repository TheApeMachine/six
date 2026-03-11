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

	// "abc" with 50% RIGHT holdout:
	//   held = int(3 * 0.5) = 1 → visible = "ab", heldOut = "c"
	first := prompt.Next()
	require.Len(t, first, 2)
	require.Equal(t, byte('a'), asByte(first[0], []byte{'a', 'b'}))
	require.Equal(t, byte('b'), asByte(first[1], []byte{'a', 'b'}))
	require.Equal(t, "ab", prompt.Value(0))
	require.Equal(t, "c", prompt.HeldOut(0))

	// "xyz" with 50% RIGHT holdout:
	//   held = 1 → visible = "xy", heldOut = "z"
	second := prompt.Next()
	require.Len(t, second, 2)
	require.Equal(t, byte('x'), asByte(second[0], []byte{'x', 'y'}))
	require.Equal(t, "xy", prompt.Value(1))
	require.Equal(t, "z", prompt.HeldOut(1))

	// Value is stable — peeking doesn't mutate.
	require.Equal(t, "ab", prompt.Value(0))
}

func TestPromptSubstringHoldout(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithDataset(promptMockDataset{samples: []string{
			"hello world",
			"test sports",
		}}),
		PromptWithSubstringHoldout([]string{" world", " sports"}),
	)

	// "hello world" → strips " world" → visible = "hello", heldOut = " world"
	first := prompt.Next()
	require.Equal(t, "hello", prompt.Value(0))
	require.Equal(t, " world", prompt.HeldOut(0))
	require.Len(t, first, 5) // "hello" = 5 chords

	// "test sports" → strips " sports" → visible = "test", heldOut = " sports"
	second := prompt.Next()
	require.Equal(t, "test", prompt.Value(1))
	require.Equal(t, " sports", prompt.HeldOut(1))
	require.Len(t, second, 4)

	// Full returns the complete original.
	require.Equal(t, "hello world", prompt.Full(0))
	require.Equal(t, "test sports", prompt.Full(1))
}

func TestPromptNoHoldout(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithDataset(promptMockDataset{samples: []string{"abc"}}),
	)

	first := prompt.Next()
	require.Len(t, first, 3) // all visible
	require.Equal(t, "abc", prompt.Value(0))
	require.Equal(t, "", prompt.HeldOut(0))
	require.Equal(t, "abc", prompt.Full(0))
}

func TestPromptExplicitValuesOverrideDatasetSamples(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithDataset(promptMockDataset{samples: []string{"dataset sample"}}),
		PromptWithValues([]string{"explicit sample"}),
	)

	require.Equal(t, 1, prompt.Len())
	require.Equal(t, "explicit sample", prompt.Value(0))
	require.Equal(t, "", prompt.HeldOut(0))

	first := prompt.Next()
	require.Len(t, first, len("explicit sample"))
	require.Equal(t, 0, prompt.Len())
}

func TestPromptExplicitSamplesOverrideDatasetSamples(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithDataset(promptMockDataset{samples: []string{"dataset sample"}}),
		PromptWithSamples([]PromptSample{
			{
				Visible: "story question?",
				HeldOut: "answer",
				Full:    "story question? answer",
			},
		}),
	)

	require.Equal(t, 1, prompt.Len())
	require.Equal(t, "story question?", prompt.Value(0))
	require.Equal(t, "answer", prompt.HeldOut(0))
	require.Equal(t, "story question? answer", prompt.Full(0))

	first := prompt.Next()
	require.Len(t, first, len("story question?"))
	require.Equal(t, 0, prompt.Len())
}

func TestPromptCenterHoldoutExposesBothVisibleSides(t *testing.T) {
	t.Parallel()

	prompt := NewPrompt(
		PromptWithValues([]string{"abcdef"}),
		PromptWithHoldout(50, CENTER),
	)

	first := prompt.Next()
	require.Len(t, first, 3)
	require.Equal(t, "aef", prompt.Value(0))
	require.Equal(t, "bcd", prompt.HeldOut(0))
	require.Equal(t, 3, prompt.MaskWidth(0))

	left, right := prompt.VisibleParts(0)
	require.Len(t, left, 1)
	require.Len(t, right, 2)
	require.Equal(t, byte('a'), asByte(left[0], []byte{'a'}))
	require.Equal(t, byte('e'), asByte(right[0], []byte{'e'}))
	require.Equal(t, byte('f'), asByte(right[1], []byte{'f'}))

	masked := prompt.MaskedVisible(0)
	require.Len(t, masked, 4)
	require.Equal(t, data.MaskChord(), masked[1])
}

func TestPromptRandomHoldoutIsDeterministic(t *testing.T) {
	t.Parallel()

	promptLeft := NewPrompt(
		PromptWithValues([]string{"abcdefghij"}),
		PromptWithHoldout(30, RANDOM),
	)
	promptRight := NewPrompt(
		PromptWithValues([]string{"abcdefghij"}),
		PromptWithHoldout(30, RANDOM),
	)

	require.Equal(t, promptLeft.Value(0), promptRight.Value(0))
	require.Equal(t, promptLeft.HeldOut(0), promptRight.HeldOut(0))
	require.Equal(t, promptLeft.MaskWidth(0), promptRight.MaskWidth(0))

	leftLo, leftHi := promptLeft.MaskRange(0)
	rightLo, rightHi := promptRight.MaskRange(0)
	require.Equal(t, leftLo, rightLo)
	require.Equal(t, leftHi, rightHi)
}
