package tokenizer

import (
	"context"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
Token is the byte-level bridge between the geometric and wave domains.
TokenID is the exact replay address; Chord is the wave-space identity used
for matching.
*/
type Token struct {
	TokenID uint64
	Z       uint8
	Pos     uint32
	Chord   data.Chord
	Events  []int
}

/*
Universal converts a byte stream from a Dataset into FibWindow-chunked
tokens. Each position in the stream produces one token per FibWindow scale.
The chord for each chunk is the OR of base chords of all bytes in the window.
*/
type Universal struct {
	ctx       context.Context
	cancel    context.CancelFunc
	coder     *MortonCoder
	dataset   provider.Dataset
	sequencer *Sequencer
	pos       uint32
}

type universalOpts func(*Universal)

func NewUniversal(opts ...universalOpts) *Universal {
	tokenizer := &Universal{
		coder:     NewMortonCoder(),
		sequencer: NewSequencer(NewCalibrator()),
		pos:       0,
	}

	for _, opt := range opts {
		opt(tokenizer)
	}

	return tokenizer
}

/*
Generate tokenizes and sequences a byte stream.

For the current memorization path, one byte occurrence becomes one Token,
and from the moment data is tokenized, we must never look at byte values
again, until it is time to render the final output.
*/
func (tokenizer *Universal) Generate() chan Token {
	out := make(chan Token)

	go func() {
		defer close(out)

		var pos uint32
		var z uint8

		for rawToken := range tokenizer.dataset.Generate() {
			chord := data.BaseChord(rawToken.Symbol)
			reset, events := tokenizer.sequencer.Analyze(int(rawToken.Pos), chord)

			out <- Token{
				TokenID: tokenizer.coder.Encode(z, pos, rawToken.Symbol),
				Z:       z,
				Pos:     pos,
				Chord:   chord,
				Events:  events,
			}

			pos++

			if reset {
				pos = 0
			}
		}
	}()

	return out
}

func TokenizerWithContext(ctx context.Context) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.ctx, tokenizer.cancel = context.WithCancel(ctx)
	}
}

func TokenizerWithCoder(coder *MortonCoder) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.coder = coder
	}
}

func TokenizerWithDataset(dataset provider.Dataset) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.dataset = dataset
	}
}

func (tokenizer *Universal) Sequencer() *Sequencer {
	return tokenizer.sequencer
}
