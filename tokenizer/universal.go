package tokenizer

import (
	"context"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/provider"
)

/*
Token carries a FibWindow chunk from the corpus.
TokenID is the Morton key for the first byte of the chunk.
Scale is the FibWindow size (3, 5, 8, 13, or 21).
Chord is the OR-aggregate of base chords for all bytes in the window.
Prompt marks tokens from the prompt path (lookup in LSM, not insertion).
*/
type Token struct {
	SampleID uint32
	TokenID  uint64
	Pos      int
	Scale    int
	Prompt   bool
	Chord    data.Chord
}

/*
Universal converts a byte stream from a Dataset into FibWindow-chunked
tokens. Each position in the stream produces one token per FibWindow scale.
The chord for each chunk is the OR of base chords of all bytes in the window.
*/
type Universal struct {
	ctx     context.Context
	cancel  context.CancelFunc
	coder   *MortonCoder
	dataset provider.Dataset
}

type universalOpts func(*Universal)

func NewUniversal(opts ...universalOpts) *Universal {
	tokenizer := &Universal{}

	for _, opt := range opts {
		opt(tokenizer)
	}

	return tokenizer
}

/*
Generate streams FibWindow-chunked tokens from the dataset.

For each position in the byte stream, it produces a token per FibWindow
scale (3, 5, 8, 13, 21). Each token's chord is the OR of base chords
for all bytes in that window.

This is where the radix trie structure emerges: the Morton key
encodes (byte_value, position), so identical prefixes share key prefixes,
and the LSM's sorted levels naturally cluster them.
*/
func (tokenizer *Universal) Generate() chan Token {
	out := make(chan Token)

	go func() {
		defer close(out)

		// Accumulate the raw byte stream separated by SampleID
		corpusBySample := make(map[uint32][]byte)
		for rawToken := range tokenizer.dataset.Generate() {
			corpusBySample[rawToken.SampleID] = append(
				corpusBySample[rawToken.SampleID], rawToken.Symbol,
			)
		}

		// Slide FibWindows across each sample individually
		for sampleID, corpus := range corpusBySample {
			for scaleIndex, scale := range numeric.FibWindows {
				if scale > len(corpus) {
					continue
				}

				for pos := 0; pos <= len(corpus)-scale; pos++ {
					window := corpus[pos : pos+scale]

					// Chord = OR of base chords for all bytes in the window
					var chord data.Chord

					for _, b := range window {
						base := BaseChord(b)
						for j := range numeric.ChordBlocks {
							chord[j] |= base[j]
						}
					}

					key := tokenizer.coder.Encode(
						uint8(scaleIndex), uint32(pos), window[0],
					)

					out <- Token{
						SampleID: sampleID,
						TokenID:  key,
						Pos:      pos,
						Scale:    scale,
						Prompt:   false,
						Chord:    chord,
					}
				}
			}
		}
	}()

	return out
}

/*
BaseChord returns a deterministic base chord for a byte value.
Uses coprime spreading to set 5 bits in the 512-bit chord,
ensuring each of the 256 byte values gets a unique signature.
*/
func BaseChord(b byte) data.Chord {
	var chord data.Chord
	totalBits := numeric.ChordBlocks * 64

	// 5 coprime multipliers spread across the chord space
	offsets := [5]int{
		int(b) * 7,
		int(b) * 13,
		int(b) * 31,
		int(b) * 61,
		int(b) * 127,
	}

	for _, off := range offsets {
		bit := off % totalBits
		chord[bit/64] |= 1 << (bit % 64)
	}

	return chord
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
