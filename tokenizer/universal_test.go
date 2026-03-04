package tokenizer

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/numeric"
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

			Convey("It should correctly generate tokens for Sample 1 across all Fibonacci windows", func() {
				var s1Tokens []Token

				for _, tk := range tokens {
					if tk.SampleID == 1 {
						s1Tokens = append(s1Tokens, tk)
					}
				}

				expectedTotal := 0

				for _, scale := range numeric.FibWindows {
					expectedTotal += len(sample1Str) - scale + 1
				}

				So(len(s1Tokens), ShouldEqual, expectedTotal)

				for scaleIndex, scale := range numeric.FibWindows {
					// Use a closure to capture the loop variables
					func(s int, idx int) {
						Convey(fmt.Sprintf("It should encode the correct Fibonacci window index (Z-depth) and position for scale %d", s), func() {
							var scaleTokens []Token

							for _, tk := range s1Tokens {
								if tk.Scale == s {
									scaleTokens = append(scaleTokens, tk)
								}
							}

							So(len(scaleTokens), ShouldEqual, len(sample1Str)-s+1)

							for i, tk := range scaleTokens {
								z, pos, sym := coder.Decode(tk.TokenID)
								So(z, ShouldEqual, uint8(idx))
								So(pos, ShouldEqual, uint32(i))
								So(tk.Pos, ShouldEqual, i)
								So(sym, ShouldEqual, sample1Str[i])
							}
						})
					}(scale, scaleIndex)
				}
			})

			Convey("It should correctly reset the sequence index to 0 when the sample ID changes (Sample 2)", func() {
				var s2Tokens []Token

				for _, tk := range tokens {
					if tk.SampleID == 2 {
						s2Tokens = append(s2Tokens, tk)
					}
				}

				// Sample 2 length is 15, covers 3, 5, 8, 13
				var scale13Tokens []Token

				for _, tk := range s2Tokens {
					if tk.Scale == 13 {
						scale13Tokens = append(scale13Tokens, tk)
					}
				}

				So(len(scale13Tokens), ShouldEqual, len(sample2Str)-13+1)

				for i, tk := range scale13Tokens {
					z, pos, sym := coder.Decode(tk.TokenID)

					So(tk.Scale, ShouldEqual, 13)
					So(z, ShouldEqual, 3)   // FibWindows[3] == 13
					So(pos, ShouldEqual, i) // Sequence index should reset to 0
					So(tk.Pos, ShouldEqual, i)
					So(sym, ShouldEqual, sample2Str[i])
				}
			})
		})
	})
}
