package huggingface

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider"
)

func TestBuildBabiQASamples(t *testing.T) {
	t.Parallel()

	samples := buildBabiQASamples(
		[]string{
			"Mary moved to the bathroom.",
			"John went to the hallway.",
			"Where is Mary?",
			"Daniel went back to the office.",
			"Where is Daniel?",
		},
		[]string{"bathroom", "office"},
		[]int{0, 0, 1, 0, 1},
	)

	Convey("Building bAbI QA samples should return valid BabiQASample slices", t, func() {
		requireLen := len(samples)
		So(requireLen, ShouldEqual, 2)

		So(samples[0].Visible, ShouldEqual, "Mary moved to the bathroom. John went to the hallway. Where is Mary?")
		So(samples[0].Answer, ShouldEqual, "bathroom")
		So(samples[0].Full, ShouldEqual, "Mary moved to the bathroom. John went to the hallway. Where is Mary?bathroom")

		So(samples[1].Visible, ShouldEqual, "Mary moved to the bathroom. John went to the hallway. Daniel went back to the office. Where is Daniel?")
		So(samples[1].Answer, ShouldEqual, "office")
		So(samples[1].Full, ShouldEqual, "Mary moved to the bathroom. John went to the hallway. Daniel went back to the office. Where is Daniel?office")
	})
}

func TestBuildBabiQASamplesFallsBackToQuestionMarks(t *testing.T) {
	t.Parallel()

	samples := buildBabiQASamples(
		[]string{
			"Mary moved to the bathroom.",
			"Where is Mary?",
		},
		[]string{"bathroom"},
		nil,
	)

	Convey("Building without type arrays should fallback to question marks", t, func() {
		So(len(samples), ShouldEqual, 1)
		So(samples[0].Visible, ShouldEqual, "Mary moved to the bathroom. Where is Mary?")
		So(samples[0].Answer, ShouldEqual, "bathroom")
		So(samples[0].Full, ShouldEqual, "Mary moved to the bathroom. Where is Mary?bathroom")
	})
}

func TestBabiQAGeneratePreservesSampleContinuity(t *testing.T) {
	t.Parallel()

	dataset := &BabiQADataset{
		samples: []BabiQASample{
			{Full: "A. B?room"},
			{Full: "C. D?hallway"},
		},
	}

	dataset.once.Do(func() {})

	var tokens []provider.RawToken
	for token := range dataset.Generate() {
		tokens = append(tokens, token)
	}

	full0 := []byte(dataset.samples[0].Full)
	full1 := []byte(dataset.samples[1].Full)

	Convey("Generating bAbI tokens should preserve sample continuity", t, func() {
		requireLen := len(tokens)
		So(requireLen, ShouldEqual, len(full0)+len(full1))

		for idx, b := range full0 {
			So(tokens[idx].SampleID, ShouldEqual, uint32(0))
			So(tokens[idx].Pos, ShouldEqual, uint32(idx))
			So(tokens[idx].Symbol, ShouldEqual, b)
		}

		offset := len(full0)
		for idx, b := range full1 {
			So(tokens[offset+idx].SampleID, ShouldEqual, uint32(1))
			So(tokens[offset+idx].Pos, ShouldEqual, uint32(idx))
			So(tokens[offset+idx].Symbol, ShouldEqual, b)
		}
	})
}
