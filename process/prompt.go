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

type Holdout struct {
	Percent int
	Type    HoldoutType
}

type Prompt struct {
	dataset  provider.Dataset
	original string
	masked   string
	heldout  Holdout
}

type promptOpts func(*Prompt)

func NewPrompt(opts ...promptOpts) *Prompt {
	prompt := &Prompt{}

	for _, opt := range opts {
		opt(prompt)
	}

	return prompt
}

func (prompt *Prompt) Samples() []string {
	return []string{}
}

func PromptWithDataset(dataset provider.Dataset) promptOpts {
	return func(p *Prompt) {
		p.dataset = dataset
	}
}

func PromptWithHoldout(prct int, ht HoldoutType) promptOpts {
	return func(p *Prompt) {
		p.heldout = Holdout{prct, ht}
	}
}
