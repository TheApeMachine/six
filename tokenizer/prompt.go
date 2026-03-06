package tokenizer

import (
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

type HoldoutType uint

const (
	RANDOM HoldoutType = iota
	TOP
	RIGHT
	BOTTOM
	LEFT
	CENTER
)

type Prompt struct {
	dataset    provider.Dataset
	tokens     [][]data.Chord
	values     [][]string
	holdout    HoldoutType
	percentage int
}

type promptOpts func(*Prompt)

func NewPrompt(opts ...promptOpts) *Prompt {
	prompt := &Prompt{}

	for _, opt := range opts {
		opt(prompt)
	}

	idx := -1

	for token := range prompt.dataset.Generate() {
		if token.Pos == 0 {
			prompt.tokens = append(prompt.tokens, make([]data.Chord, 0))
			prompt.values = append(prompt.values, make([]string, 0))
			idx++
		}

		if idx >= 0 {
			prompt.tokens[idx] = append(
				prompt.tokens[idx], data.BaseChord(token.Symbol),
			)

			prompt.values[idx] = append(
				prompt.values[idx], string(token.Symbol),
			)
		}
	}

	return prompt
}

func (prompt *Prompt) Next() (out []data.Chord) {
	if len(prompt.tokens) == 0 {
		return nil
	}

	// Popping stacks for breakfast.
	prompt.tokens, out = prompt.tokens[1:], prompt.tokens[0]

	if len(out) > 0 && prompt.percentage > 0 {
		keep := int(float64(len(out)) * float64(100-prompt.percentage) / 100.0)
		if keep > 0 {
			switch prompt.holdout {
			case RIGHT, BOTTOM:
				out = out[:keep]
			case LEFT, TOP:
				out = out[len(out)-keep:]
			default:
				out = out[:keep]
			}
		}
	}

	return out
}

func (prompt *Prompt) Value(idx int) string {
	return strings.Join(prompt.values[idx], "")
}

func PromptWithDataset(dataset provider.Dataset) promptOpts {
	return func(prompt *Prompt) {
		prompt.dataset = dataset
	}
}

func PromptWithHoldout(percentage int, holdoutType HoldoutType) promptOpts {
	return func(prompt *Prompt) {
		prompt.holdout = holdoutType
		prompt.percentage = percentage
	}
}
