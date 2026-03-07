package tokenizer

import (
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
HoldoutType selects which region of each sample is masked (held out).

  - RIGHT:     remove pct% from the END.
  - LEFT:      remove pct% from the START.
  - SUBSTRING: remove exact substring matches (e.g. label names).
*/
type HoldoutType uint

const (
	RANDOM HoldoutType = iota
	TOP
	RIGHT
	BOTTOM
	LEFT
	CENTER
	SUBSTRING
)

/*
sample carries the precomputed visible and held-out portions of a prompt.
All slicing is done at materialisation time in flush(), so Next/Value/HeldOut
are simple lookups.
*/
type sample struct {
	visible []data.Chord // what Next() returns
	visStr  []string     // parallel to visible, joined by Value()
	heldOut string       // what HeldOut() returns
	fullStr []string     // the complete original, joined by Full()
}

/*
Prompt synchronously materialises a dataset into a stack of samples,
each split into a visible part (returned by Next) and a held-out target
(returned by Value/HeldOut).
*/
type Prompt struct {
	dataset    provider.Dataset
	samples    []sample
	cursor     int
	holdout    HoldoutType
	percentage int
	substrings []string // for SUBSTRING holdout
	values     []string // for explicit values
}

type promptOpts func(*Prompt)

func NewPrompt(opts ...promptOpts) *Prompt {
	p := &Prompt{
		substrings: []string{},
		values:     []string{},
	}

	for _, opt := range opts {
		opt(p)
	}

	for _, v := range p.values {
		var chords []data.Chord
		var values []string
		for _, b := range []byte(v) {
			chords = append(chords, data.BaseChord(b))
			values = append(values, string(b))
		}
		p.samples = append(p.samples, p.buildSample(chords, values))
	}

	// Explicit values define the evaluation set; do not silently append
	// dataset-derived prompts on top of them.
	if p.dataset != nil && len(p.values) == 0 {
		var (
			chords []data.Chord
			values []string
		)

		flush := func() {
			if len(chords) == 0 {
				return
			}
			p.samples = append(p.samples, p.buildSample(chords, values))
			chords, values = nil, nil
		}

		for token := range p.dataset.Generate() {
			if token.Pos == 0 && len(chords) > 0 {
				flush()
			}
			chords = append(chords, data.BaseChord(token.Symbol))
			values = append(values, string(token.Symbol))
		}
		flush()
	}

	return p
}

/*
buildSample splits chords/values into visible and held-out portions
based on the configured holdout type.
*/
func (p *Prompt) buildSample(chords []data.Chord, values []string) sample {
	full := make([]string, len(values))
	copy(full, values)

	switch p.holdout {
	case SUBSTRING:
		return p.buildSubstringSample(chords, values, full)
	default:
		return p.buildPositionalSample(chords, values, full)
	}
}

func (p *Prompt) buildPositionalSample(chords []data.Chord, values []string, full []string) sample {
	n := len(chords)
	if p.percentage <= 0 || n == 0 {
		// No holdout — everything visible.
		return sample{visible: chords, visStr: values, fullStr: full}
	}

	held := min(max(int(float64(n)*float64(p.percentage)/100.0), 1), n)

	switch p.holdout {
	case LEFT, TOP:
		// Hold out the first `held` elements.
		return sample{
			visible: chords[held:],
			visStr:  values[held:],
			heldOut: strings.Join(values[:held], ""),
			fullStr: full,
		}
	default:
		// Hold out the last `held` elements.
		cut := n - held
		return sample{
			visible: chords[:cut],
			visStr:  values[:cut],
			heldOut: strings.Join(values[cut:], ""),
			fullStr: full,
		}
	}
}

func (p *Prompt) buildSubstringSample(chords []data.Chord, values []string, full []string) sample {
	text := strings.Join(values, "")

	// Find which substring appears and strip it.
	for _, sub := range p.substrings {
		idx := strings.LastIndex(text, sub)
		if idx < 0 {
			continue
		}

		// Map the byte index back to the values/chords slice index.
		// Each value is a single byte (one character), so byte index = slice index.
		end := idx + len(sub)
		vis := make([]data.Chord, 0, len(chords)-len(sub))
		visStr := make([]string, 0, len(values)-len(sub))

		vis = append(vis, chords[:idx]...)
		vis = append(vis, chords[end:]...)
		visStr = append(visStr, values[:idx]...)
		visStr = append(visStr, values[end:]...)

		return sample{
			visible: vis,
			visStr:  visStr,
			heldOut: sub,
			fullStr: full,
		}
	}

	// No substring found — nothing held out.
	return sample{visible: chords, visStr: values, fullStr: full}
}

/*
Next pops the next sample, returning only the visible chords.
Returns nil when the stack is exhausted.
*/
func (p *Prompt) Next() []data.Chord {
	if p.cursor >= len(p.samples) {
		return nil
	}

	s := p.samples[p.cursor]
	p.cursor++
	return s.visible
}

/*
Value returns the visible portion of the sample at idx as a string.
Same content as Next() but without advancing the cursor.
*/
func (p *Prompt) Value(idx int) string {
	if idx < 0 || idx >= len(p.samples) {
		return ""
	}
	return strings.Join(p.samples[idx].visStr, "")
}

/*
HeldOut returns the masked portion of the sample at idx as a string.
*/
func (p *Prompt) HeldOut(idx int) string {
	if idx < 0 || idx >= len(p.samples) {
		return ""
	}
	return p.samples[idx].heldOut
}

/*
Full returns the complete, unsplit string for the sample at idx.
*/
func (p *Prompt) Full(idx int) string {
	if idx < 0 || idx >= len(p.samples) {
		return ""
	}
	return strings.Join(p.samples[idx].fullStr, "")
}

/*
Len returns the number of remaining samples.
*/
func (p *Prompt) Len() int {
	return len(p.samples) - p.cursor
}

func PromptWithDataset(dataset provider.Dataset) promptOpts {
	return func(p *Prompt) {
		p.dataset = dataset
	}
}

func PromptWithHoldout(percentage int, holdoutType HoldoutType) promptOpts {
	return func(p *Prompt) {
		p.holdout = holdoutType
		p.percentage = percentage
	}
}

// PromptWithSubstringHoldout configures SUBSTRING holdout mode.
// Any occurrence of the given strings will be stripped from each sample's
// visible portion and stored as the held-out target.
func PromptWithSubstringHoldout(substrings []string) promptOpts {
	return func(p *Prompt) {
		p.holdout = SUBSTRING
		p.substrings = substrings
	}
}

func PromptWithValues(values []string) promptOpts {
	return func(p *Prompt) {
		p.values = values
	}
}
