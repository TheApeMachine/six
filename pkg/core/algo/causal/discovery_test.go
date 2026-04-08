package causal

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDiscoveryLearn(t *testing.T) {
	t.Parallel()

	Convey("Learn runs PC passes without panicking", t, func() {
		graph := NewGraph()

		for idx := range 6 {
			label := "a"

			if idx%2 == 1 {
				label = "b"
			}

			pred, release := graphPrediction(label, 10, 20, "s", "t")
			_, err := graph.Update(pred)
			release()

			So(err, ShouldBeNil)
		}

		discovery := NewDiscovery(graph)

		So(func() {
			discovery.Learn(2, 0.05)
		}, ShouldNotPanic)
	})
}

func TestDiscoveryIsDirectCause(t *testing.T) {
	t.Parallel()

	Convey("IsDirectCause is false before orientation", t, func() {
		graph := NewGraph()
		discovery := NewDiscovery(graph)

		So(discovery.IsDirectCause(1, 2), ShouldBeFalse)
	})
}

func TestDiscoveryCausalChildren(t *testing.T) {
	t.Parallel()

	Convey("CausalChildren empty on fresh discovery", t, func() {
		discovery := NewDiscovery(NewGraph())

		So(discovery.CausalChildren(1), ShouldHaveLength, 0)
	})
}

func TestDiscoverySeparationSet(t *testing.T) {
	t.Parallel()

	Convey("SeparationSet nil when pair never tested", t, func() {
		discovery := NewDiscovery(NewGraph())

		So(discovery.SeparationSet(1, 2), ShouldBeNil)
	})
}

func TestEnumerateSubsets(t *testing.T) {
	t.Parallel()

	Convey("enumerateSubsets size zero yields empty set only", t, func() {
		So(enumerateSubsets([]uint64{9, 8, 7}, 0), ShouldResemble, [][]uint64{{}})
	})

	Convey("enumerateSubsets returns C(n,k) combinations", t, func() {
		items := []uint64{1, 2, 3, 4}

		So(len(enumerateSubsets(items, 2)), ShouldEqual, 6)
	})
}

func TestChiSquaredSurvival(t *testing.T) {
	t.Parallel()

	Convey("chiSquaredSurvival treats non-positive inputs as full tail mass", t, func() {
		So(chiSquaredSurvival(0, 3), ShouldEqual, 1.0)
		So(chiSquaredSurvival(-1, 3), ShouldEqual, 1.0)
		So(chiSquaredSurvival(5, 0), ShouldEqual, 1.0)
	})

	Convey("chiSquaredSurvival shrinks as the statistic grows", t, func() {
		lo := chiSquaredSurvival(1, 8)
		hi := chiSquaredSurvival(10, 8)

		So(lo, ShouldBeGreaterThan, hi)
		So(hi, ShouldBeGreaterThan, 0)
		So(lo, ShouldBeLessThan, 1)
		So(math.IsNaN(lo), ShouldBeFalse)
		So(math.IsNaN(hi), ShouldBeFalse)
	})
}

func TestRegularizedGammaP(t *testing.T) {
	t.Parallel()

	Convey("regularizedGammaP returns zero for non-positive x", t, func() {
		So(regularizedGammaP(2.5, 0), ShouldEqual, 0)
	})

	Convey("regularizedGammaP increases along x on the series branch", t, func() {
		low := regularizedGammaP(4, 0.5)
		high := regularizedGammaP(4, 2)

		So(low, ShouldBeLessThan, high)
		So(high, ShouldBeLessThan, 1)
		So(math.IsNaN(low), ShouldBeFalse)
		So(math.IsNaN(high), ShouldBeFalse)
	})
}

func TestDiscoveryLearnDefaultParameters(t *testing.T) {
	t.Parallel()

	Convey("Learn substitutes defaults when maxConditionSize and alpha are non-positive", t, func() {
		graph := NewGraph()

		for idx := range 8 {
			label := "a"

			if idx%2 == 1 {
				label = "b"
			}

			pred, release := graphPrediction(label, 30, 40, "u", "v")
			_, err := graph.Update(pred)
			release()

			So(err, ShouldBeNil)
		}

		discovery := NewDiscovery(graph)

		So(func() {
			discovery.Learn(0, 0)
		}, ShouldNotPanic)
	})
}

func BenchmarkEnumerateSubsets(b *testing.B) {
	items := []uint64{1, 2, 3, 4, 5, 6}

	b.ResetTimer()

	for b.Loop() {
		_ = enumerateSubsets(items, 3)
	}
}

func BenchmarkChiSquaredSurvival(b *testing.B) {
	x := 12.3
	df := 5.0

	var p float64

	b.ResetTimer()

	for b.Loop() {
		p = chiSquaredSurvival(x, df)
	}

	if math.IsNaN(p) {
		b.Fatal("unexpected NaN")
	}
}

func BenchmarkDiscoveryLearn(b *testing.B) {
	graph := NewGraph()

	for idx := range 8 {
		label := "a"

		if idx%2 == 1 {
			label = "b"
		}

		pred, release := graphPrediction(label, 50+uint64(idx), 60, "m", "n")
		_, _ = graph.Update(pred)
		release()
	}

	discovery := NewDiscovery(graph)

	b.ResetTimer()

	for b.Loop() {
		discovery.Learn(2, 0.05)
	}
}
