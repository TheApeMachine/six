package process

import (
	"bytes"
	"math/rand"

	"github.com/theapemachine/six/pkg/provider"
)

/*
HoldoutType enumerates the strategies for masking part of a prompt's content.
*/
type HoldoutType uint

const (
	NONE   HoldoutType = iota
	TOP                // alias for RIGHT in byte terms (leading bytes)
	RIGHT              // mask trailing bytes
	BOTTOM             // alias for LEFT in byte terms (trailing bytes)
	LEFT               // mask leading bytes
	CENTER             // mask middle bytes
	RANDOM             // mask randomly selected bytes
	MATCH              // mask bytes matching a pattern
)

/*
Holdout holds the masking configuration applied to each prompt.
*/
type Holdout struct {
	Percent int
	Type    HoldoutType
	Match   []byte
}

/*
Prompt sequences samples from either a static list or a streaming dataset,
applying a holdout mask to each before exposing it for consumption.
*/
type Prompt struct {
	dataset   provider.Dataset
	prompts   []string
	original  string
	masked    string
	heldout   Holdout
	promptIdx int
	datasetCh chan provider.RawToken
	hasNext   bool
	nextTkn   provider.RawToken
}

/*
promptOpts is a functional option for Prompt.
*/
type promptOpts func(*Prompt)

/*
NewPrompt instantiates a Prompt with the supplied options.
*/
func NewPrompt(opts ...promptOpts) *Prompt {
	prompt := &Prompt{}

	for _, opt := range opts {
		opt(prompt)
	}

	return prompt
}

/*
PromptWithDataset configures the Prompt to stream samples from a Dataset.
*/
func PromptWithDataset(dataset provider.Dataset) promptOpts {
	return func(prom *Prompt) {
		prom.dataset = dataset
	}
}

/*
PromptWithStrings configures the Prompt with a static list of samples.
*/
func PromptWithStrings(prompts []string) promptOpts {
	return func(prom *Prompt) {
		prom.prompts = prompts
	}
}

/*
PromptWithHoldout configures the masking strategy and percentage.
*/
func PromptWithHoldout(prct int, ht HoldoutType) promptOpts {
	return func(prom *Prompt) {
		prom.heldout.Percent = prct
		prom.heldout.Type = ht
	}
}

/*
PromptWithMatch sets the byte pattern used by the MATCH holdout strategy.
*/
func PromptWithMatch(match []byte) promptOpts {
	return func(prom *Prompt) {
		prom.heldout.Match = match
	}
}

/*
Next advances to the next sample. Returns false when the source is exhausted.
For dataset mode it groups consecutive tokens that share a SampleID.
*/
func (prompt *Prompt) Next() bool {
	if prompt.dataset != nil {
		return prompt.nextFromDataset()
	}

	return prompt.nextFromStrings()
}

/*
nextFromDataset reads the next group of tokens sharing a SampleID.
*/
func (prompt *Prompt) nextFromDataset() bool {
	if prompt.datasetCh == nil {
		prompt.datasetCh = prompt.dataset.Generate()

		tkn, ok := <-prompt.datasetCh
		if !ok {
			return false
		}

		prompt.nextTkn = tkn
		prompt.hasNext = true
	}

	if !prompt.hasNext {
		return false
	}

	currentID := prompt.nextTkn.SampleID
	buf := []byte{prompt.nextTkn.Symbol}

	prompt.hasNext = false

	for tkn := range prompt.datasetCh {
		if tkn.SampleID != currentID {
			prompt.nextTkn = tkn
			prompt.hasNext = true
			break
		}

		buf = append(buf, tkn.Symbol)
	}

	prompt.original = string(buf)
	prompt.applyHoldout()

	return true
}

/*
nextFromStrings advances through the static prompts slice.
*/
func (prompt *Prompt) nextFromStrings() bool {
	if prompt.promptIdx >= len(prompt.prompts) {
		return false
	}

	prompt.original = prompt.prompts[prompt.promptIdx]
	prompt.promptIdx++
	prompt.applyHoldout()

	return true
}

/*
applyHoldout derives prompt.masked from prompt.original using the holdout config.
When no masking is configured masked equals original.
*/
func (prompt *Prompt) applyHoldout() {
	if prompt.heldout.Type == NONE || (prompt.heldout.Percent == 0 && prompt.heldout.Type != MATCH) {
		prompt.masked = prompt.original
		return
	}

	raw := []byte(prompt.original)
	n := len(raw)

	if n == 0 {
		prompt.masked = ""
		return
	}

	count := max((n*prompt.heldout.Percent)/100, 1)

	switch prompt.heldout.Type {
	case TOP, RIGHT:
		prompt.masked = string(raw[:n-count])
	case BOTTOM, LEFT:
		prompt.masked = string(raw[count:])
	case CENTER:
		start := (n - count) / 2
		prompt.masked = string(append(raw[:start], raw[start+count:]...))
	case RANDOM:
		res := make([]byte, n)
		copy(res, raw)

		for _, idx := range rand.Perm(n)[:count] {
			res[idx] = 0
		}

		prompt.masked = string(res)
	case MATCH:
		if len(prompt.heldout.Match) > 0 {
			prompt.masked = string(
				bytes.ReplaceAll(
					raw,
					prompt.heldout.Match,
					make([]byte, len(prompt.heldout.Match)),
				),
			)
		} else {
			prompt.masked = string(raw)
		}
	default:
		prompt.masked = string(raw)
	}
}

/*
Original returns the unmasked sample text after the last Next() call.
*/
func (prompt *Prompt) Original() string {
	return prompt.original
}

/*
Masked returns the holdout-masked sample text after the last Next() call.
*/
func (prompt *Prompt) Masked() string {
	return prompt.masked
}
