package tokenizer

import (
	"context"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
)

/*
Token is the output of the tokenizer — the BOUNDARY between bytes and geometry.

Once a Token is emitted, nothing downstream may use raw bytes. The TokenID
is the Morton-coded replay address (stored in the LSM with the Chord); the
Chord is the sole geometric identity used for all downstream matching,
reasoning, and recall.
*/
type Token struct {
	TokenID    uint64
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
	ctx          context.Context
	cancel       context.CancelFunc
	coder        *MortonCoder
	dataset      provider.Dataset
	sequencer    *Sequencer
	pos          uint32
	tokens       strings.Builder
	sampleID     uint32
	useSequencer bool
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
		defer func() {
			select {
			case out <- Token{IsBoundary: true}:
			default:
			}
		}()

		tokenizer.pos = 0
		var streamPos uint32

		for rawToken := range tokenizer.dataset.Generate() {
			var reset bool

			if rawToken.SampleID != tokenizer.sampleID {
				console.Trace(
					"tokenizer-boundary",
					"sequence", tokenizer.tokens.String(),
				)

				reset = true

				tokenizer.sampleID = rawToken.SampleID
				tokenizer.tokens.Reset()
				tokenizer.pos = 0

				if tokenizer.useSequencer {
					tokenizer.sequencer = NewSequencer(NewCalibrator())
				}
			}

			chord := data.BaseChord(rawToken.Symbol)

			var events []int

			if tokenizer.useSequencer {
				seqReset, seqEvents := tokenizer.sequencer.Analyze(
					int(tokenizer.pos), rawToken.Symbol,
				)
				events = seqEvents

				if seqReset {
					reset = true
				}

				tokenizer.pos++

				if seqReset {
					tokenizer.pos = 0
				}
			}

			tokenizer.tokens.WriteByte(rawToken.Symbol)

			out <- Token{
				TokenID:    tokenizer.coder.Encode(streamPos, rawToken.Symbol),
				Chord:      chord,
				Events:     events,
				IsBoundary: reset,
			}

			streamPos++
			tokenizer.pos++
		}
	}()

	return out
}

func (tokenizer *Universal) Sequencer() *Sequencer {
	return tokenizer.sequencer
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

func TokenizerWithSequencer() universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.useSequencer = true
	}
}
