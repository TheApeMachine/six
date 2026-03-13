package process

import (
	"bytes"
	"math/rand"

	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
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

After each Next() call, Chords() returns the chunk chords produced by
the real Tokenizer — same chunking and encoding as training data, but
without inserting into the LSM.
*/
type Prompt struct {
	err       error
	tokenizer *TokenizerServer
	dataset   provider.Dataset
	prompts   []string
	original  string
	masked    string
	heldout   Holdout
	promptIdx int
	datasetCh chan provider.RawToken
	hasNext   bool
	nextTkn   provider.RawToken
	rng       *rand.Rand
}

/*
Option is a functional option for Prompt.
*/
type Option func(*Prompt)

/*
NewPrompt instantiates a Prompt with the supplied options.
*/
func NewPrompt(opts ...Option) *Prompt {
	p := &Prompt{
		rng: rand.New(rand.NewSource(1)),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

/*
Next advances to the next sample. Returns false when the source is exhausted.
After returning true, Chords() contains the chunk chords for the sample.
*/
func (prompt *Prompt) Next() bool {
	var ok bool

	if prompt.dataset != nil {
		ok = prompt.nextFromDataset()
	} else {
		ok = prompt.nextFromStrings()
	}

	return ok
}

/*
Error returns the error state of the Prompt.
*/
func (prompt *Prompt) Error() error {
	return prompt.err
}

/*
Chords returns the chunk chords for the current sample, produced by
the real Tokenizer with useSampleID mode.
*/
func (prompt *Prompt) Chords() []data.Chord {
	if prompt.tokenizer == nil || len(prompt.tokenizer.collector) == 0 {
		return nil
	}

	// The prompt tokenizer uses a single sample (index 0) per Next() call.
	if len(prompt.tokenizer.collector[0]) == 0 {
		return nil
	}

	return prompt.tokenizer.collector[0]
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
	prompt.ApplyHoldout()
	prompt.tokenizeMasked()

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
	prompt.ApplyHoldout()
	prompt.tokenizeMasked()

	return true
}

/*
tokenizeMasked feeds the masked text through the real Tokenizer with
useSampleID=true and a collector. Same chunking path as training, no
LSM insertion.
*/
func (prompt *Prompt) tokenizeMasked() {
	if prompt.tokenizer == nil {
		return
	}

	if err := prompt.tokenizer.TokenizeSingleSample(prompt.tokenizer.ctx, prompt.masked); err != nil {
		prompt.err = err
	}
}

/*
applyHoldout derives p.masked from p.original using the holdout config.
When no masking is configured masked equals original.
*/
func (prompt *Prompt) ApplyHoldout() {
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

	count := (n * prompt.heldout.Percent) / 100

	switch prompt.heldout.Type {
	case RIGHT:
		prompt.masked = string(raw[:n-count])
	case LEFT:
		prompt.masked = string(raw[count:])
	case CENTER:
		start := (n - count) / 2
		prompt.masked = string(append(raw[:start], raw[start+count:]...))
	case RANDOM:
		res := make([]byte, n)
		copy(res, raw)

		for _, idx := range prompt.rng.Perm(n)[:count] {
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
PromptWithOriginal sets the original text for the prompt directly, bypassing generators.
*/
func PromptWithOriginal(original string) Option {
	return func(p *Prompt) {
		p.original = original
	}
}

/*
PromptWithTokenizer injects the Tokenizer used to produce chunk chords.
The tokenizer should be constructed with a pool and broadcast but does
NOT need a dataset or spatial insert — the Prompt feeds it per-sample.
*/
func PromptWithTokenizer(tokenizer *TokenizerServer) Option {
	return func(p *Prompt) {
		p.tokenizer = tokenizer
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
