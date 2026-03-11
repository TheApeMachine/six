package vm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

type loaderMockDataset struct {
	samples []string
}

func (dataset *loaderMockDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)

	go func() {
		defer close(out)

		for sampleID, sample := range dataset.samples {
			for pos := range len(sample) {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   sample[pos],
					Pos:      uint32(pos),
				}
			}
		}
	}()

	return out
}

func TestLoaderBuildsDirectionalSubstrates(t *testing.T) {
	Convey("Given a loader backed by a lexical dataset", t, func() {
		dataset := &loaderMockDataset{samples: []string{"abc", "de"}}
		loader := NewLoader(
			LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(dataset),
				),
			),
		)

		Convey("When the loader ingests the corpus", func() {
			err := loader.Start()

			Convey("It should build both forward and reverse substrates and preserve lexical sequences", func() {
				So(err, ShouldBeNil)
				So(loader.Substrate(), ShouldNotBeNil)
				So(loader.ReverseSubstrate(), ShouldNotBeNil)
				So(len(loader.Substrate().Entries), ShouldBeGreaterThan, 0)
				So(len(loader.ReverseSubstrate().Entries), ShouldBeGreaterThan, 0)
				So(loader.ReverseSubstrate().Entries[0].Reverse, ShouldBeTrue)

				sequences := loader.Sequences()
				So(sequences, ShouldHaveLength, 1)
				So(sequences[0][0], ShouldEqual, data.BaseChord('a'))
				stopIdx := -1
				for idx, chord := range sequences[0] {
					if chord.IsStopChord() {
						stopIdx = idx
						break
					}
				}
				So(stopIdx, ShouldBeGreaterThan, 0)
				So(stopIdx+1, ShouldBeLessThan, len(sequences[0]))
				So(sequences[0][stopIdx+1], ShouldEqual, data.BaseChord('d'))
			})
		})
	})
}

func BenchmarkLoaderBuildDirectionalSubstrates(b *testing.B) {
	loader := NewLoader()
	sequence := make([]data.Chord, 0, 256)
	lexical := make([]data.Chord, 0, 256)

	for idx := 0; idx < 256; idx++ {
		chord := data.BaseChord(byte(idx))
		sequence = append(sequence, chord.BindGeometry(idx, nil))
		lexical = append(lexical, chord)
	}

	for b.Loop() {
		loader.substrate.Entries = loader.substrate.Entries[:0]
		loader.reverseSubstrate.Entries = loader.reverseSubstrate.Entries[:0]
		loader.buildDirectionalSubstrates(sequence, lexical, 0)
	}
}
