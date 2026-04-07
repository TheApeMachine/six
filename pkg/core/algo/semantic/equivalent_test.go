package semantic

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestNewEquivalent(t *testing.T) {
	t.Parallel()

	Convey("NewEquivalent preserves constructor fields", t, func() {
		vocab := []string{"one"}
		co := map[string]map[string]float64{}

		engine := NewEquivalent("a", "b", 0.5, vocab, co)

		So(engine.Original, ShouldEqual, "a")
		So(engine.Mapped, ShouldEqual, "b")
		So(engine.Similarity, ShouldEqual, 0.5)
	})
}

func TestEquivalentRun(t *testing.T) {
	t.Parallel()

	Convey("Run maps identical token to itself with similarity 1 when co-occurrence matches", t, func() {
		vocab := []string{"same", "other"}
		co := map[string]map[string]float64{
			"same":  {"other": 1.0},
			"other": {"same": 1.0},
		}

		engine := NewEquivalent("", "", 0, vocab, co)
		got := engine.Run("same")

		So(got.Mapped, ShouldEqual, "same")
		So(got.Similarity, ShouldEqual, core.Cfg.MarkovTrie.EditSimilarity)
	})

	Convey("Run falls back to Levenshtein when lengths differ within window", t, func() {
		saved := core.Cfg.MarkovTrie.EditDistance
		core.Cfg.MarkovTrie.EditDistance = 2

		defer func() {
			core.Cfg.MarkovTrie.EditDistance = saved
		}()

		vocab := []string{"cat", "car"}
		co := map[string]map[string]float64{}

		engine := NewEquivalent("", "", 0, vocab, co)
		got := engine.Run("car")

		So(got.Mapped, ShouldEqual, "car")
		So(got.Original, ShouldEqual, "car")
	})
}

func BenchmarkEquivalentRun(b *testing.B) {
	vocab := make([]string, 64)

	for idx := range vocab {
		vocab[idx] = string(rune('a' + (idx % 26)))
	}

	co := map[string]map[string]float64{}

	for idx := range vocab {
		left := vocab[idx]
		co[left] = map[string]float64{vocab[(idx+1)%len(vocab)]: 1}
	}

	engine := NewEquivalent("", "", 0, vocab, co)
	word := vocab[32]

	b.ResetTimer()

	for b.Loop() {
		_ = engine.Run(word)
	}
}
