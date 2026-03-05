package tokenizer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/provider"
)

type MockDataset struct {
	Tokens []provider.RawToken
}

func (mds *MockDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)

	go func() {
		defer close(out)

		for _, t := range mds.Tokens {
			out <- t
		}
	}()

	return out
}

func createMockDataset(samples ...string) *MockDataset {
	var tokens []provider.RawToken

	for i, sample := range samples {
		sampleID := uint32(i + 1)

		for _, char := range sample {
			tokens = append(tokens, provider.RawToken{
				SampleID: sampleID,
				Symbol:   byte(char),
			})
		}
	}

	return &MockDataset{Tokens: tokens}
}

func TestGenerate(t *testing.T) {
	Convey("Given a Dataset with multiple samples", t, func() {
		// Sample 1: length > 21 to cover all FibWindows (3, 5, 8, 13, 21)
		sample1Str := "this is a sufficiently long string to test all windows" // len 54
		// Sample 2: length 15 to cover FibWindows (3, 5, 8, 13)
		sample2Str := "a second sample" // len 15

		dataset := createMockDataset(sample1Str, sample2Str)

		coder := NewMortonCoder()
		tokenizer := NewUniversal(
			TokenizerWithCoder(coder),
			TokenizerWithDataset(dataset),
		)

		Convey("When Generate is called", func() {
			tokens := make([]Token, 0)

			for token := range tokenizer.Generate() {
				tokens = append(tokens, token)
			}

			Convey("It should dynamically chunk Sample 1 using topological boundaries (modality agnostic)", func() {
				var s1Tokens []Token

				for _, tk := range tokens {
					if tk.SampleID == 1 {
						s1Tokens = append(s1Tokens, tk)
					}
				}

				So(len(s1Tokens), ShouldBeGreaterThan, 0)
				
				// Assert that sequence Index starts correctly
				resets := 0
				for _, tk := range s1Tokens {
					if tk.Pos == 0 {
						resets++
					}
					z, pos, _ := coder.Decode(tk.TokenID)
					// Z depth should match the scale
					So(z, ShouldEqual, uint8(tk.Scale))
					So(pos, ShouldEqual, uint32(tk.Pos))
				}
				So(resets, ShouldBeGreaterThan, 0)
			})

			Convey("It should correctly reset the sequence index to 0 when the sample ID changes (Sample 2)", func() {
				var s2Tokens []Token

				for _, tk := range tokens {
					if tk.SampleID == 2 {
						s2Tokens = append(s2Tokens, tk)
					}
				}

				So(len(s2Tokens), ShouldBeGreaterThan, 0)
				So(s2Tokens[0].Pos, ShouldEqual, 0) // The first token in sample 2 should start at pos 0
			})
		})
	})
}
