package tokenizer

import (
	"context"
	"strings"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
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
	Bound      data.Chord
	Carrier    data.Chord
	Rotation   geometry.GFRotation
	Events     []int
	Pos        uint32
	Position   uint32
	IsBoundary bool
}

/*
EffectiveChord returns the geometry-bound chord when present, otherwise the
atomic base chord.
*/
func (token Token) EffectiveChord() data.Chord {
	if token.Bound.ActiveCount() > 0 {
		return token.Bound
	}

	return token.Chord
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
	rot          geometry.GFRotation
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
		rot:       geometry.IdentityRotation(),
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
		tokenizer.rot = geometry.IdentityRotation()
		var streamPos uint32
		var hasSample bool

		resetSample := func(sampleID uint32) {
			tokenizer.sampleID = sampleID
			tokenizer.tokens.Reset()
			tokenizer.pos = 0
			tokenizer.rot = geometry.IdentityRotation()

			if tokenizer.useSequencer {
				tokenizer.sequencer = NewSequencer(NewCalibrator())
			}
		}

		for rawToken := range tokenizer.dataset.Generate() {
			if !hasSample {
				hasSample = true
				resetSample(rawToken.SampleID)
			}

			if rawToken.SampleID != tokenizer.sampleID {
				console.Trace(
					"tokenizer-boundary",
					"sequence", tokenizer.tokens.String(),
				)

				if tokenizer.tokens.Len() > 0 {
					out <- Token{IsBoundary: true}
				}

				resetSample(rawToken.SampleID)
			}

			currentPos := tokenizer.pos
			baseChord := data.BaseChord(rawToken.Symbol)
			carrier := tokenizer.rot.StateChord()
			rotated := tokenizer.rot.ApplyToChord(baseChord)
			bound := rotated.BindGeometry(int(currentPos), &carrier)

			var events []int
			var reset bool

			if tokenizer.useSequencer {
				seqReset, seqEvents := tokenizer.sequencer.Analyze(
					int(currentPos), rawToken.Symbol,
				)
				events = seqEvents
				reset = seqReset
			}

			tokenizer.tokens.WriteByte(rawToken.Symbol)

			out <- Token{
				TokenID:    tokenizer.coder.Encode(streamPos, rawToken.Symbol),
				Chord:      baseChord,
				Bound:      bound,
				Carrier:    carrier,
				Rotation:   tokenizer.rot,
				Events:     events,
				Pos:        currentPos,
				Position:   currentPos,
				IsBoundary: reset,
			}

			streamPos++

			nextRot := tokenizer.rot.Compose(geometry.RotationForChord(bound))
			if len(events) > 0 {
				nextRot = nextRot.Compose(geometry.ComposeEvents(events))
			}

			tokenizer.rot = nextRot

			if reset {
				tokenizer.pos = 0
				tokenizer.rot = geometry.IdentityRotation()
				continue
			}

			tokenizer.pos = currentPos + 1
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
