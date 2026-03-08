package tokenizer

import (
	"context"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
Token is the byte-level bridge between the geometric and wave domains.
TokenID is the exact replay address in the observed stream; Pos is the
sequencer-local position inside the current discovered segment. Chord is
the wave-space identity used for matching.
*/
type Token struct {
	TokenID    uint64
	Pos        uint32
	SampleID   uint32
	Chord      data.Chord
	Events     []int
	IsBoundary bool
}

/*
Universal converts a byte stream from a Dataset into position-bound byte
tokens. Each byte occurrence becomes one Token, and RollLeft binds the
sequencer-local position directly into the chord geometry.
*/
type Universal struct {
	ctx       context.Context
	cancel    context.CancelFunc
	coder     *MortonCoder
	dataset   provider.Dataset
	sequencer *Sequencer
	pos       uint32
	tokens    strings.Builder
	sampleID  uint32
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
	out := make(chan Token, 4096)
	if tokenizer.dataset == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)

		tokenizer.pos = 0
		var streamPos uint32

		for rawToken := range tokenizer.dataset.Generate() {
			if rawToken.SampleID != tokenizer.sampleID {
				tokenizer.sampleID = rawToken.SampleID
				tokenizer.tokens.Reset()
				console.Trace(
					"tokenizer-boundary",
					"sequence", tokenizer.tokens.String(),
				)
				tokenizer.pos = 0
			}

			chord := data.BaseChord(rawToken.Symbol)
			reset, events := tokenizer.sequencer.Analyze(int(tokenizer.pos), rawToken.Symbol)

			tokenizer.tokens.WriteByte(rawToken.Symbol)

			out <- Token{
				TokenID:    tokenizer.coder.Encode(streamPos, rawToken.Symbol),
				Pos:        tokenizer.pos,
				SampleID:   rawToken.SampleID,
				Chord:      chord,
				Events:     events,
				IsBoundary: reset,
			}

			streamPos++
			tokenizer.pos++
		}

		// Emit the trailing data after the last boundary as a final sequence.
		if tokenizer.tokens.Len() > 0 {
			console.Trace(
				"tokenizer-boundary",
				"sequence", tokenizer.tokens.String(),
			)
			tokenizer.tokens.Reset()
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
