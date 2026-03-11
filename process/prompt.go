package process

import "github.com/theapemachine/six/provider"

type HoldoutType uint

const (
	NONE HoldoutType = iota
	TOP
	RIGHT
	BOTTOM
	LEFT
	CENTER
	RANDOM
	MATCH
)

type Prompt struct {
	original string
	masked   string
	heldout  string
}

type promptOpts func(*Prompt)

func NewPrompt(opts ...promptOpts) *Prompt {
	prompt := &Prompt{}

	for _, opt := range opts {
		opt(prompt)
	}

	return prompt
}

func PromptWithDataset(dataset provider.Dataset) {

}

func PromptWithHoldout(prct int, ht HoldoutType) {

}
