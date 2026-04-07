package beam

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewRankedTokens(t *testing.T) {
	t.Parallel()

	Convey("NewRankedTokens allocates zero-mass entries", t, func() {
		ranked := NewRankedTokens([]string{"u", "v"})

		So(len(ranked), ShouldEqual, 2)
		So(ranked[0].Probability, ShouldEqual, 0)
		So(ranked[1].Token, ShouldEqual, "v")
	})
}

func TestRankedTokensSortDescending(t *testing.T) {
	t.Parallel()

	Convey("SortDescending breaks ties lexicographically on token", t, func() {
		ranked := RankedTokens{
			{Token: "b", Probability: 0.5},
			{Token: "a", Probability: 0.5},
		}

		ranked.SortDescending()

		So(ranked[0].Token, ShouldEqual, "a")
		So(ranked[1].Token, ShouldEqual, "b")
	})
}

func BenchmarkRankedTokensSortDescending(b *testing.B) {
	ranked := make(RankedTokens, 128)

	for idx := range ranked {
		ranked[idx] = RankedToken{
			Token:       string(rune('a' + (idx % 26))),
			Probability: float64(idx%17) / 100,
		}
	}

	b.ResetTimer()

	for b.Loop() {
		ranked.SortDescending()
	}
}
