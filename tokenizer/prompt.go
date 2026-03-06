package tokenizer

import (
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
HoldoutType selects which region of each sample is masked for evaluation.
*/
type HoldoutType uint

const (
	RANDOM HoldoutType = iota
	TOP
	RIGHT
	BOTTOM
	LEFT
	CENTER
)

/*
sample is a single prompt entry that carries its full chord sequence,
the partition boundary, and the original string values in lockstep.
*/
type sample struct {
	chords []data.Chord // full sequence
	values []string     // full string tokens
	keep   int          // index boundary: chords[:keep] = foundation, values[keep:] = holdout
}

/*
Prompt synchronously materialises a dataset into a popping stack of
samples, each split into a foundation (returned by Next) and a holdout
target (returned by Value).  The full prompt is always stored; only the
split point determines what the caller sees.
*/
type Prompt struct {
	dataset    provider.Dataset
	samples    []sample
	cursor     int
	holdout    HoldoutType
	percentage int
}

type promptOpts func(*Prompt)

func NewPrompt(opts ...promptOpts) *Prompt {
	p := &Prompt{}

	for _, opt := range opts {
		opt(p)
	}

	// Materialise every sample from the dataset.
	var (
		chords []data.Chord
		values []string
	)

	flush := func() {
		if len(chords) == 0 {
			return
		}
		k := splitPoint(len(chords), p.percentage, p.holdout)
		p.samples = append(p.samples, sample{
			chords: chords,
			values: values,
			keep:   k,
		})
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

	return p
}

/*
splitPoint returns the index that separates foundation from holdout.
*/
func splitPoint(n, pct int, ht HoldoutType) int {
	if pct <= 0 || n == 0 {
		return n // no holdout → keep everything
	}

	keep := min(max(int(float64(n)*float64(100-pct)/100.0), 0), n)

	switch ht {
	case LEFT, TOP:
		// holdout is at the start → foundation is the tail
		return n - keep
	default: // RIGHT, BOTTOM, RANDOM, CENTER all default to tail holdout
		return keep
	}
}

/*
Next pops the next sample, returning only the foundation chords.

Returns nil when the stack is exhausted.
*/
func (p *Prompt) Next() []data.Chord {
	if p.cursor >= len(p.samples) {
		return nil
	}

	s := p.samples[p.cursor]
	p.cursor++

	switch {
	case s.keep == len(s.chords):
		return s.chords
	case s.keep == 0:
		return s.chords[:0] // holdout everything
	default:
		return s.chords[:s.keep]
	}
}

/*
Value returns the held-out target string for the sample at idx.

The holdout region is the complement of what Next returned.
*/
func (p *Prompt) Value(idx int) string {
	if idx < 0 || idx >= len(p.samples) {
		return ""
	}

	s := p.samples[idx]

	switch {
	case s.keep >= len(s.values):
		return "" // nothing held out
	case s.keep == 0:
		return strings.Join(s.values, "")
	default:
		return strings.Join(s.values[s.keep:], "")
	}
}

/*
Full returns the complete, unsplit string for the sample at idx.
*/
func (p *Prompt) Full(idx int) string {
	if idx < 0 || idx >= len(p.samples) {
		return ""
	}

	return strings.Join(p.samples[idx].values, "")
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
