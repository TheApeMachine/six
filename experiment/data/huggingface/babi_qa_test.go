package huggingface

import (
	"github.com/theapemachine/six/experiment/data"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
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

	var tokens []byte
	for b := range dataset.Generate() {
		tokens = append(tokens, b)
	}

	full0 := []byte(dataset.samples[0].Full)
	full1 := []byte(dataset.samples[1].Full)

	Convey("Generating bAbI tokens should preserve sample continuity", t, func() {
		requireLen := len(tokens)
		So(requireLen, ShouldEqual, len(full0)+len(full1))

		for idx, b := range full0 {
			So(tokens[idx], ShouldEqual, b)
		}

		offset := len(full0)
		for idx, b := range full1 {
			So(tokens[offset+idx], ShouldEqual, b)
		}
	})
}

func TestBabiQAGeneratePromptsEmitsStructuredSamples(t *testing.T) {
	t.Parallel()

	dataset := &BabiQADataset{
		samples: []BabiQASample{
			{Visible: "Alice?where", Answer: "garden", Full: "full1"},
			{Visible: "Bob?where", Answer: "office", Full: "full2"},
		},
	}
	dataset.once.Do(func() {})

	prompts := make([]data.Prompt, 0)
	for sample := range dataset.GeneratePrompts() {
		prompts = append(prompts, sample)
	}

	Convey("When generating bAbI prompts", t, func() {
		Convey("It should preserve sample boundary metadata", func() {
			So(len(prompts), ShouldEqual, 2)
			So(prompts[0].SampleID, ShouldEqual, 0)
			So(prompts[0].Text, ShouldEqual, "Alice?where")
			So(prompts[0].Label, ShouldEqual, "garden")
			So(prompts[0].HasLabel, ShouldBeTrue)

			So(prompts[1].SampleID, ShouldEqual, 1)
			So(prompts[1].Text, ShouldEqual, "Bob?where")
			So(prompts[1].Label, ShouldEqual, "office")
		})
	})
}
