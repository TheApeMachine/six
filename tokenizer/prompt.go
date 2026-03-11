package tokenizer

import (
	"hash/fnv"
	"strings"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
HoldoutType selects which region of each sample is masked (held out).

  - RIGHT:     remove pct% from the END.
  - LEFT:      remove pct% from the START.
  - CENTER:    remove pct% from the MIDDLE.
  - RANDOM:    remove pct% from a deterministic interior span.
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
	visible []data.Chord
	visStr  []string
	heldOut string
	fullStr []string

	left     []data.Chord
	right    []data.Chord
	leftStr  []string
	rightStr []string
	maskLo   int
	maskHi   int
}

/*
PromptSample defines Visible (prompt), HeldOut (target), and Full (complete) for explicit samples.
*/
type PromptSample struct {
	Visible string
	HeldOut string
	Full    string
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
	substrings []string
	values     []string
	explicit   []PromptSample
}

type promptOpts func(*Prompt)

/*
NewPrompt builds a Prompt from opts. With dataset: materializes samples from Generate().
With values: one sample per string. With explicit: uses PromptSample slices directly.
*/
func NewPrompt(opts ...promptOpts) *Prompt {
	prompt := &Prompt{
		substrings: []string{},
		values:     []string{},
	}

	for _, opt := range opts {
		opt(prompt)
	}

	for _, value := range prompt.values {
		var chords []data.Chord
		var values []string

		for _, symbol := range []byte(value) {
			chords = append(chords, data.BaseChord(symbol))
			values = append(values, string(symbol))
		}

		prompt.samples = append(prompt.samples, prompt.buildSample(chords, values))
	}

	for _, explicit := range prompt.explicit {
		full := explicit.Full
		if full == "" {
			full = explicit.Visible + explicit.HeldOut
		}

		visibleChords := stringsToChords(explicit.Visible)
		visibleStrings := bytesToStrings([]byte(explicit.Visible))
		prompt.samples = append(prompt.samples, sample{
			visible: copyChords(visibleChords),
			visStr:  copyStrings(visibleStrings),
			heldOut: explicit.HeldOut,
			fullStr: bytesToStrings([]byte(full)),
			left:    copyChords(visibleChords),
			leftStr: copyStrings(visibleStrings),
			maskLo:  len(visibleChords),
			maskHi:  len(visibleChords) + len([]byte(explicit.HeldOut)),
		})
	}

	if prompt.dataset != nil && len(prompt.values) == 0 && len(prompt.explicit) == 0 {
		var (
			chords []data.Chord
			values []string
		)

		flush := func() {
			if len(chords) == 0 {
				return
			}

			prompt.samples = append(prompt.samples, prompt.buildSample(chords, values))
			chords, values = nil, nil
		}

		for token := range prompt.dataset.Generate() {
			if token.Pos == 0 && len(chords) > 0 {
				flush()
			}

			chords = append(chords, data.BaseChord(token.Symbol))
			values = append(values, string(token.Symbol))
		}

		flush()
	}

	return prompt
}

/*
buildSample splits chords/values into visible and held-out portions
based on the configured holdout type.
*/
func (prompt *Prompt) buildSample(chords []data.Chord, values []string) sample {
	full := make([]string, len(values))
	copy(full, values)

	switch prompt.holdout {
	case SUBSTRING:
		return prompt.buildSubstringSample(chords, values, full)
	default:
		return prompt.buildPositionalSample(chords, values, full)
	}
}

func (prompt *Prompt) buildPositionalSample(chords []data.Chord, values []string, full []string) sample {
	n := len(chords)
	if prompt.percentage <= 0 || n == 0 {
		return newSample(chords, nil, values, nil, "", full, 0, 0)
	}

	held := min(max(int(float64(n)*float64(prompt.percentage)/100.0), 1), n)

	switch prompt.holdout {
	case LEFT, TOP:
		return newSample(
			nil,
			chords[held:],
			nil,
			values[held:],
			strings.Join(values[:held], ""),
			full,
			0,
			held,
		)

	case CENTER:
		start := max((n-held)/2, 0)
		end := min(start+held, n)

		return newSample(
			chords[:start],
			chords[end:],
			values[:start],
			values[end:],
			strings.Join(values[start:end], ""),
			full,
			start,
			end,
		)

	case RANDOM:
		start := prompt.randomSpanStart(values, held)
		end := min(start+held, n)

		return newSample(
			chords[:start],
			chords[end:],
			values[:start],
			values[end:],
			strings.Join(values[start:end], ""),
			full,
			start,
			end,
		)

	default:
		cut := n - held

		return newSample(
			chords[:cut],
			nil,
			values[:cut],
			nil,
			strings.Join(values[cut:], ""),
			full,
			cut,
			n,
		)
	}
}

func (prompt *Prompt) buildSubstringSample(chords []data.Chord, values []string, full []string) sample {
	text := strings.Join(values, "")

	for _, substring := range prompt.substrings {
		idx := strings.LastIndex(text, substring)
		if idx < 0 {
			continue
		}

		end := idx + len(substring)

		return newSample(
			chords[:idx],
			chords[end:],
			values[:idx],
			values[end:],
			substring,
			full,
			idx,
			end,
		)
	}

	return newSample(chords, nil, values, nil, "", full, 0, 0)
}

func (prompt *Prompt) randomSpanStart(values []string, held int) int {
	n := len(values)
	if held <= 0 || n <= held {
		return 0
	}

	seed := strings.Join(values, "")
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(seed))
	spanCount := n - held + 1
	if spanCount <= 1 {
		return 0
	}

	return int(hasher.Sum32() % uint32(spanCount))
}

func newSample(left, right []data.Chord, leftStr, rightStr []string, heldOut string, full []string, lo, hi int) sample {
	visible := concatChords(left, right)
	visStr := concatStrings(leftStr, rightStr)

	return sample{
		visible:  visible,
		visStr:   visStr,
		heldOut:  heldOut,
		fullStr:  copyStrings(full),
		left:     copyChords(left),
		right:    copyChords(right),
		leftStr:  copyStrings(leftStr),
		rightStr: copyStrings(rightStr),
		maskLo:   lo,
		maskHi:   hi,
	}
}

/*
Next pops the next sample, returning only the visible chords.
Returns nil when the stack is exhausted.
*/
func (prompt *Prompt) Next() []data.Chord {
	if prompt.cursor >= len(prompt.samples) {
		return nil
	}

	sample := prompt.samples[prompt.cursor]
	prompt.cursor++

	return copyChords(sample.visible)
}

/*
Value returns the visible portion of the sample at idx as a string.
Same content as Next() but without advancing the cursor.
*/
func (prompt *Prompt) Value(idx int) string {
	if idx < 0 || idx >= len(prompt.samples) {
		return ""
	}

	return strings.Join(prompt.samples[idx].visStr, "")
}

/*
HeldOut returns the masked portion of the sample at idx as a string.
*/
func (prompt *Prompt) HeldOut(idx int) string {
	if idx < 0 || idx >= len(prompt.samples) {
		return ""
	}

	return prompt.samples[idx].heldOut
}

/*
Full returns the complete, unsplit string for the sample at idx.
*/
func (prompt *Prompt) Full(idx int) string {
	if idx < 0 || idx >= len(prompt.samples) {
		return ""
	}

	return strings.Join(prompt.samples[idx].fullStr, "")
}

/*
VisibleParts returns the left and right visible regions around the masked span.
*/
func (prompt *Prompt) VisibleParts(idx int) ([]data.Chord, []data.Chord) {
	if idx < 0 || idx >= len(prompt.samples) {
		return nil, nil
	}

	sample := prompt.samples[idx]

	return copyChords(sample.left), copyChords(sample.right)
}

/*
VisibleStrings returns the visible text on the left and right side of the mask.
*/
func (prompt *Prompt) VisibleStrings(idx int) (string, string) {
	if idx < 0 || idx >= len(prompt.samples) {
		return "", ""
	}

	sample := prompt.samples[idx]

	return strings.Join(sample.leftStr, ""), strings.Join(sample.rightStr, "")
}

/*
MaskedVisible returns the visible prompt with a dedicated gap marker inserted
between the left and right regions when a masked interior span exists.
*/
func (prompt *Prompt) MaskedVisible(idx int) []data.Chord {
	left, right := prompt.VisibleParts(idx)
	if len(right) == 0 {
		return left
	}

	out := make([]data.Chord, 0, len(left)+1+len(right))
	out = append(out, left...)
	out = append(out, data.MaskChord())
	out = append(out, right...)

	return out
}

/*
MaskWidth returns the width of the held-out span in chords.
*/
func (prompt *Prompt) MaskWidth(idx int) int {
	if idx < 0 || idx >= len(prompt.samples) {
		return 0
	}

	sample := prompt.samples[idx]
	if sample.maskHi <= sample.maskLo {
		return 0
	}

	return sample.maskHi - sample.maskLo
}

/*
MaskRange returns the half-open [start, end) coordinates of the masked span
inside the full sample.
*/
func (prompt *Prompt) MaskRange(idx int) (int, int) {
	if idx < 0 || idx >= len(prompt.samples) {
		return 0, 0
	}

	sample := prompt.samples[idx]

	return sample.maskLo, sample.maskHi
}

/*
Len returns the number of remaining samples.
*/
func (prompt *Prompt) Len() int {
	return len(prompt.samples) - prompt.cursor
}

func stringsToChords(text string) []data.Chord {
	chords := make([]data.Chord, 0, len(text))

	for _, symbol := range []byte(text) {
		chords = append(chords, data.BaseChord(symbol))
	}

	return chords
}

func bytesToStrings(raw []byte) []string {
	values := make([]string, 0, len(raw))

	for _, symbol := range raw {
		values = append(values, string(symbol))
	}

	return values
}

func copyChords(chords []data.Chord) []data.Chord {
	if len(chords) == 0 {
		return nil
	}

	out := make([]data.Chord, len(chords))
	copy(out, chords)

	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, len(values))
	copy(out, values)

	return out
}

func concatChords(left, right []data.Chord) []data.Chord {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}

	out := make([]data.Chord, 0, len(left)+len(right))
	out = append(out, left...)
	out = append(out, right...)

	return out
}

func concatStrings(left, right []string) []string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}

	out := make([]string, 0, len(left)+len(right))
	out = append(out, left...)
	out = append(out, right...)

	return out
}

/*
PromptWithDataset sets the dataset; samples are built from dataset.Generate() on NewPrompt.
*/
func PromptWithDataset(dataset provider.Dataset) promptOpts {
	return func(prompt *Prompt) {
		prompt.dataset = dataset
	}
}

/*
PromptWithHoldout sets percentage (0-100) and type (LEFT,RIGHT,CENTER,etc.) for splitting.
*/
func PromptWithHoldout(percentage int, holdoutType HoldoutType) promptOpts {
	return func(prompt *Prompt) {
		prompt.holdout = holdoutType
		prompt.percentage = percentage
	}
}

/*
PromptWithSubstringHoldout sets SUBSTRING mode; strips first matching substring from visible,
stores it as HeldOut. Uses LastIndex (rightmost match).
*/
func PromptWithSubstringHoldout(substrings []string) promptOpts {
	return func(prompt *Prompt) {
		prompt.holdout = SUBSTRING
		prompt.substrings = substrings
	}
}

/*
PromptWithValues adds one sample per string. Each character becomes a BaseChord.
*/
func PromptWithValues(values []string) promptOpts {
	return func(prompt *Prompt) {
		prompt.values = values
	}
}

/*
PromptWithSamples uses explicit PromptSample slices. Overrides dataset/values if both set.
*/
func PromptWithSamples(samples []PromptSample) promptOpts {
	return func(prompt *Prompt) {
		prompt.explicit = samples
	}
}
