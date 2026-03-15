package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestWavefrontSearchPromptEditAlignment(t *testing.T) {
	gc.Convey("Given a phase-indexed prompt corpus", t, func() {
		idx := buildPhaseIndex([]byte("Roy is in the Kitchen"))
		wf := NewWavefront(
			idx,
			WavefrontWithMaxHeads(64),
			WavefrontWithMaxDepth(64),
		)

		cases := []struct {
			name   string
			prompt []byte
		}{
			{name: "substitution typo", prompt: []byte("Roy is in the Kitchan")},
			{name: "deleted prompt byte", prompt: []byte("Roy is in the Kithen")},
			{name: "inserted prompt byte", prompt: []byte("Roy is in the Kittchen")},
			{name: "partial prompt", prompt: []byte("Roy is in the Kitch")},
		}

		for _, tc := range cases {
			tc := tc

			gc.Convey(tc.name+" should keep the Kitchen branch alive", func() {
				results := wf.SearchPrompt(tc.prompt, nil, nil)
				gc.So(len(results), gc.ShouldBeGreaterThan, 0)

				decoded := idx.decodeChords(results[0].Path)
				gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
				gc.So(string(decoded[0]), gc.ShouldContainSubstring, "Kitchen")
			})
		}
	})
}

func BenchmarkWavefrontSearchPromptEdits(b *testing.B) {
	idx := buildPhaseIndex([]byte("Roy is in the Kitchen"))
	wf := NewWavefront(
		idx,
		WavefrontWithMaxHeads(64),
		WavefrontWithMaxDepth(64),
	)
	prompt := []byte("Roy is in the Kithen")

	for b.Loop() {
		_ = wf.SearchPrompt(prompt, nil, nil)
	}
}
