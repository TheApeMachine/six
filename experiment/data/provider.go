package data

import "iter"

type Provider interface {
	Generate() iter.Seq[byte]
}

/*
Prompt is a unit of supervised or unsupervised work for an experiment.
Text is one sample payload; Label is optional supervision metadata.
*/
type Prompt struct {
	SampleID uint32
	Text     string
	Label    string
	HasLabel bool
}

/*
PromptProvider emits structured samples without lossy byte concatenation.
It is useful when experiments need prompt boundaries and optional labels.
*/
type PromptProvider interface {
	GeneratePrompts() iter.Seq[Prompt]
}
