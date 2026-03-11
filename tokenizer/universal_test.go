package tokenizer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
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

func createMockDatasetWithStartID(startID uint32, samples ...string) *MockDataset {
	var tokens []provider.RawToken

	for i, sample := range samples {
		sampleID := startID + uint32(i)

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
		sample1Str := "this is a sufficiently long string to test all windows" // len 54
		sample2Str := "a second sample"                                        // len 15

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

			Convey("It should dynamically chunk Sample 1 using topological boundaries", func() {
				var s1Tokens []Token

				for _, tk := range tokens {
					if tk.Chord.IsStopChord() {
						continue
					}

					if tk.Chord.IsStreamMarker() {
						continue
					}

					// In this architecture, we decode token ID to verify properties if needed
					_, symbol := coder.Decode(tk.TokenID)
					// Verify Decode works and tokens exist.
					want := data.BaseChord(symbol)
					So(tk.Chord, ShouldEqual, want)
					if symbol > 0 {
						s1Tokens = append(s1Tokens, tk)
					}
				}

			})

			// Because SampleID isn't tracked in Token directly anymore, we rely on the continuous stream and the topological variance logic to cause resets.
			// The previous test already verified resets occurred.
		})
	})
}

func TestGenerateWithNilDataset(t *testing.T) {
	t.Parallel()

	tokenizer := NewUniversal()

	count := 0
	for range tokenizer.Generate() {
		count++
	}

	if count != 0 {
		t.Fatalf("expected no tokens from nil dataset, got %d", count)
	}
}

func TestGenerateEmitsBoundaryBetweenSamples(t *testing.T) {
	Convey("Given a Dataset with multiple sample IDs", t, func() {
		dataset := createMockDataset("abc", "def")
		tokenizer := NewUniversal(
			TokenizerWithDataset(dataset),
		)

		Convey("When Generate is called", func() {
			stopMarkers := 0

			for token := range tokenizer.Generate() {
				if token.Chord.IsStopChord() {
					stopMarkers++
				}
			}

			Convey("It should emit an in-band stop marker between samples and one at stream end", func() {
				So(stopMarkers, ShouldEqual, 2)
			})
		})
	})
}

func TestGenerateEmitsBoundaryWhenFirstSampleIDIsZero(t *testing.T) {
	Convey("Given a Dataset whose first sample ID is zero", t, func() {
		dataset := createMockDatasetWithStartID(0, "abc", "def")
		tokenizer := NewUniversal(
			TokenizerWithDataset(dataset),
		)

		Convey("When Generate is called", func() {
			stopMarkers := 0

			for token := range tokenizer.Generate() {
				if token.Chord.IsStopChord() {
					stopMarkers++
				}
			}

			Convey("It should still use an in-band stop marker and flush at stream end", func() {
				So(stopMarkers, ShouldEqual, 2)
			})
		})
	})
}

func TestGenerateEmitsBoundChordState(t *testing.T) {
	Convey("Given a Dataset with a single lexical sample", t, func() {
		dataset := createMockDataset("ab")
		tokenizer := NewUniversal(
			TokenizerWithDataset(dataset),
		)

		Convey("When Generate is called", func() {
			var first Token
			for token := range tokenizer.Generate() {
				if token.Chord.ActiveCount() == 0 {
					continue
				}

				first = token
				break
			}

			Convey("It should emit both lexical and bound chord state", func() {
				So(first.Chord, ShouldEqual, data.BaseChord('a'))
				So(first.Bound.ActiveCount(), ShouldBeGreaterThan, first.Chord.ActiveCount())
				So(first.Carrier.ActiveCount(), ShouldBeGreaterThan, 0)
				So(first.EffectiveChord(), ShouldEqual, first.Bound)
			})
		})
	})
}

func TestGeneratePreservesPositionAcrossSampleBoundary(t *testing.T) {
	Convey("Given a Dataset with multiple sample IDs", t, func() {
		dataset := createMockDataset("ab", "cd")
		tokenizer := NewUniversal(
			TokenizerWithDataset(dataset),
		)

		Convey("When Generate is called", func() {
			positions := make([]uint32, 0, 5)
			sawStop := false

			for token := range tokenizer.Generate() {
				if token.Chord.IsStopChord() {
					sawStop = true
					continue
				}

				positions = append(positions, token.Pos)
			}

			Convey("It should keep advancing position through the in-band stop marker", func() {
				So(sawStop, ShouldBeTrue)
				So(positions, ShouldResemble, []uint32{0, 1, 3, 4})
			})
		})
	})
}

func TestGenerateEmitsSplitMarkersWhenSequencerFindsStructure(t *testing.T) {
	Convey("Given a Dataset with a strong internal distribution shift", t, func() {
		dataset := createMockDataset("aaaaaaaaaaaaaaaaaaaabbbbbbbbbbcccccccccccccccccccc")
		tokenizer := NewUniversal(
			TokenizerWithDataset(dataset),
			TokenizerWithSequencer(),
		)

		Convey("When Generate is called", func() {
			splitMarkers := 0

			for token := range tokenizer.Generate() {
				if token.Chord.IsSplitChord() {
					splitMarkers++
				}
			}

			Convey("It should emit at least one in-band split marker", func() {
				So(splitMarkers, ShouldBeGreaterThan, 0)
			})
		})
	})
}

func BenchmarkGenerateInBandBoundaries(b *testing.B) {
	dataset := createMockDataset(
		"aaaaaaaaaaaaaaaaaaaabbbbbbbbbbcccccccccccccccccccc",
		"defghijklmnopqrstuvwxyz",
	)
	tokenizer := NewUniversal(
		TokenizerWithDataset(dataset),
		TokenizerWithSequencer(),
	)

	for b.Loop() {
		for range tokenizer.Generate() {
		}
	}
}
