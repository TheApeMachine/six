package data

import (
	"io"
	"iter"
)

type Provider interface {
	io.ReadCloser
	Generate() iter.Seq[Sample]
}

/*
Sample is a unit of supervised or unsupervised work for an experiment.

Text is the byte stream fed to the tokenizer for this row (training line,
including any staged suffix the dataset adds). Label is optional supervision
metadata. Prompt, when non-empty, is the task-facing string harnesses should
use in Prompts() (question-only, article without a classification suffix, etc.);
when empty, TaskPrompt falls back to Text.
*/
type Sample struct {
	SampleID uint32
	Text     []byte
	Label    []byte
	LabelInt uint64
	Prompt   []byte
}

/*
TaskPrompt returns the experiment prompt bytes: Prompt when set, otherwise Text.
*/
func (sample Sample) TaskPrompt() []byte {
	if len(sample.Prompt) > 0 {
		return sample.Prompt
	}

	return sample.Text
}

