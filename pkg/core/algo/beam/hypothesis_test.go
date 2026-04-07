package beam

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHypothesisExtend(t *testing.T) {
	t.Parallel()

	Convey("Extend returns the input when branchFactor is zero", t, func() {
		root := &Hypothesis{Tokens: []string{"a"}, Score: 0}
		ranked := RankedTokens{{Token: "b", Probability: 0.4}}

		out := root.Extend(ranked, 0)

		So(len(out), ShouldEqual, 1)
		So(out[0], ShouldEqual, root)
	})

	Convey("Extend accumulates log probability mass", t, func() {
		root := &Hypothesis{Tokens: []string{}, Score: 0}
		ranked := RankedTokens{
			{Token: "b", Probability: 0.5},
		}

		out := root.Extend(ranked, 2)

		So(len(out), ShouldEqual, 1)
		So(out[0].Tokens[len(out[0].Tokens)-1], ShouldEqual, "b")
	})
}

func TestHypothesisPrune(t *testing.T) {
	t.Parallel()

	Convey("Prune keeps top scores only", t, func() {
		seed := &Hypothesis{}
		hyps := []*Hypothesis{
			{Score: 1},
			{Score: 9},
			{Score: 5},
		}

		pruned := seed.Prune(hyps, 2)

		So(len(pruned), ShouldEqual, 2)
		So(pruned[0].Score, ShouldEqual, 9)
		So(pruned[1].Score, ShouldEqual, 5)
	})
}

func TestHypothesisLayerOpen(t *testing.T) {
	t.Parallel()

	Convey("LayerOpen is false when every path ends on end token", t, func() {
		seed := &Hypothesis{}

		open := seed.LayerOpen([]*Hypothesis{
			{Tokens: []string{"end"}},
		}, "end")

		So(open, ShouldBeFalse)
	})
}

func TestContinuations(t *testing.T) {
	t.Parallel()

	Convey("Continuations strips prefix tokens and end markers", t, func() {
		hyps := []*Hypothesis{{
			Tokens: []string{"a", "b", "</s>", "c"},
			Score:  -1,
		}}

		out := Continuations(2, hyps, "</s>", " ")

		So(len(out), ShouldEqual, 1)
		So(out[0].Sequence, ShouldEqual, "c")
	})
}

func BenchmarkHypothesisExtend(b *testing.B) {
	root := &Hypothesis{Tokens: make([]string, 4), Score: -0.5}

	for idx := range root.Tokens {
		root.Tokens[idx] = "x"
	}

	ranked := make(RankedTokens, 32)

	for idx := range ranked {
		ranked[idx] = RankedToken{
			Token:       string(rune('a' + (idx % 26))),
			Probability: 1.0 / float64(len(ranked)),
		}
	}

	width := 8

	b.ResetTimer()

	for b.Loop() {
		_ = root.Extend(ranked, width)
	}
}
