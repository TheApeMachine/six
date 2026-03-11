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
Universal converts a byte stream from a Dataset into position-bound chord
tokens. Lexical bytes become Tokens, and in-band boundary markers may also be
emitted to preserve temporal continuity without relying on hard external resets.
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

For the current memorization path, lexical bytes become Tokens and structural
markers may be inserted in-band. From the moment data is tokenized, we must
never look at byte values again until final rendering.
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
			case out <- Token{Chord: data.StopChord()}:
			default:
			}
		}()

		tokenizer.pos = 0
		tokenizer.rot = geometry.IdentityRotation()
		var streamPos uint32
		var hasSample bool

		switchSample := func(sampleID uint32) {
			tokenizer.sampleID = sampleID
			tokenizer.tokens.Reset()

			if tokenizer.useSequencer {
				tokenizer.sequencer = NewSequencer(NewCalibrator())
			}
		}

		for rawToken := range tokenizer.dataset.Generate() {
			if !hasSample {
				hasSample = true
				switchSample(rawToken.SampleID)
			}

			if rawToken.SampleID != tokenizer.sampleID {
				console.Trace(
					"tokenizer-boundary",
					"sequence", tokenizer.tokens.String(),
				)

				if tokenizer.tokens.Len() > 0 {
					tokenizer.emitMarker(out, data.StopChord(), &streamPos)
				}

				switchSample(rawToken.SampleID)
			}

			currentPos := tokenizer.pos
			baseChord := data.BaseChord(rawToken.Symbol)
			carrier := tokenizer.rot.StateChord()
			rotated := tokenizer.rot.ApplyToChord(baseChord)
			bound := rotated.BindGeometry(int(currentPos), &carrier)

			var events []int
			var split bool

			if tokenizer.useSequencer {
				seqReset, seqEvents := tokenizer.sequencer.Analyze(
					int(currentPos), rawToken.Symbol,
				)
				events = seqEvents
				split = seqReset
			}

			tokenizer.tokens.WriteByte(rawToken.Symbol)

			out <- Token{
				TokenID:  tokenizer.coder.Encode(streamPos, rawToken.Symbol),
				Chord:    baseChord,
				Bound:    bound,
				Carrier:  carrier,
				Rotation: tokenizer.rot,
				Events:   events,
				Pos:      currentPos,
				Position: currentPos,
			}

			streamPos++

			nextRot := tokenizer.rot.Compose(geometry.RotationForChord(bound))
			if len(events) > 0 {
				nextRot = nextRot.Compose(geometry.ComposeEvents(events))
			}

			tokenizer.rot = nextRot
			tokenizer.pos = currentPos + 1

			if split {
				tokenizer.emitMarker(out, data.SplitChord(), &streamPos)
			}
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

func (tokenizer *Universal) emitMarker(
	out chan Token,
	marker data.Chord,
	streamPos *uint32,
) {
	currentPos := tokenizer.pos
	carrier := tokenizer.rot.StateChord()
	bound := marker.BindGeometry(int(currentPos), &carrier)

	out <- Token{
		TokenID:  tokenizer.coder.Encode(*streamPos, markerTokenByte(marker)),
		Chord:    marker,
		Bound:    bound,
		Carrier:  carrier,
		Rotation: tokenizer.rot,
		Pos:      currentPos,
		Position: currentPos,
	}

	*streamPos = *streamPos + 1
	tokenizer.rot = tokenizer.rot.Compose(geometry.RotationForChord(bound))
	tokenizer.pos = currentPos + 1
}

func markerTokenByte(marker data.Chord) byte {
	if marker.IsSplitChord() {
		return 255
	}

	return 0
}
