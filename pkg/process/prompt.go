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
	RIGHT              // mask trailing bytes
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
	err       error
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
Option is a functional option for Prompt.
*/
type Option func(*Prompt)

/*
NewPrompt instantiates a Prompt with the supplied options.
*/
func NewPrompt(opts ...Option) *Prompt {
	p := &Prompt{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

/*
Next advances to the next sample. Returns false when the source is exhausted.
For dataset mode it groups consecutive tokens that share a SampleID.
*/
func (p *Prompt) Next() bool {
	if p.dataset != nil {
		return p.nextFromDataset()
	}

	return p.nextFromStrings()
}

/*
Error returns the error state of the Prompt.
*/
func (p *Prompt) Error() error {
	return p.err
}

/*
nextFromDataset reads the next group of tokens sharing a SampleID.
*/
func (p *Prompt) nextFromDataset() bool {
	if p.datasetCh == nil {
		p.datasetCh = p.dataset.Generate()

		tkn, ok := <-p.datasetCh
		if !ok {
			return false
		}

		p.nextTkn = tkn
		p.hasNext = true
	}

	if !p.hasNext {
		return false
	}

	currentID := p.nextTkn.SampleID
	buf := []byte{p.nextTkn.Symbol}

	p.hasNext = false

	for tkn := range p.datasetCh {
		if tkn.SampleID != currentID {
			p.nextTkn = tkn
			p.hasNext = true
			break
		}

		buf = append(buf, tkn.Symbol)
	}

	p.original = string(buf)
	p.applyHoldout()

	return true
}

/*
nextFromStrings advances through the static prompts slice.
*/
func (p *Prompt) nextFromStrings() bool {
	if p.promptIdx >= len(p.prompts) {
		return false
	}

	p.original = p.prompts[p.promptIdx]
	p.promptIdx++
	p.applyHoldout()

	return true
}

/*
applyHoldout derives p.masked from p.original using the holdout config.
When no masking is configured masked equals original.
*/
func (p *Prompt) applyHoldout() {
	if p.heldout.Type == NONE || (p.heldout.Percent == 0 && p.heldout.Type != MATCH) {
		p.masked = p.original
		return
	}

	raw := []byte(p.original)
	n := len(raw)

	if n == 0 {
		p.masked = ""
		return
	}

	count := max((n*p.heldout.Percent)/100, 1)

	switch p.heldout.Type {
	case RIGHT:
		p.masked = string(raw[:n-count])
	case LEFT:
		p.masked = string(raw[count:])
	case CENTER:
		start := (n - count) / 2
		p.masked = string(append(raw[:start], raw[start+count:]...))
	case RANDOM:
		res := make([]byte, n)
		copy(res, raw)

		for _, idx := range rand.Perm(n)[:count] {
			res[idx] = 0
		}

		p.masked = string(res)
	case MATCH:
		if len(p.heldout.Match) > 0 {
			p.masked = string(
				bytes.ReplaceAll(
					raw,
					p.heldout.Match,
					make([]byte, len(p.heldout.Match)),
				),
			)
		} else {
			p.masked = string(raw)
		}
	default:
		p.masked = string(raw)
	}
}

/*
Original returns the unmasked sample text after the last Next() call.
*/
func (p *Prompt) Original() string {
	return p.original
}

/*
Masked returns the holdout-masked sample text after the last Next() call.
*/
func (p *Prompt) Masked() string {
	return p.masked
}

/*
PromptWithDataset configures the Prompt to stream samples from a Dataset.
*/
func PromptWithDataset(dataset provider.Dataset) Option {
	return func(p *Prompt) {
		p.dataset = dataset
	}
}

/*
PromptWithStrings configures the Prompt with a static list of samples.
*/
func PromptWithStrings(prompts []string) Option {
	return func(p *Prompt) {
		p.prompts = prompts
	}
}

/*
PromptWithHoldout configures the masking strategy and percentage.
*/
func PromptWithHoldout(prct int, ht HoldoutType) Option {
	return func(p *Prompt) {
		p.heldout.Percent = prct
		p.heldout.Type = ht
	}
}

/*
PromptWithMatch sets the byte pattern used by the MATCH holdout strategy.
*/
func PromptWithMatch(match []byte) Option {
	return func(p *Prompt) {
		p.heldout.Match = match
	}
}
