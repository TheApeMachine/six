package causal

import (
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

			_, err := graph.Update(graphPrediction(label, 10, 20, "s", "t"))

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

func BenchmarkDiscoveryLearn(b *testing.B) {
	graph := NewGraph()

	for idx := range 8 {
		label := "a"

		if idx%2 == 1 {
			label = "b"
		}

		_, _ = graph.Update(graphPrediction(label, 50+uint64(idx), 60, "m", "n"))
	}

	discovery := NewDiscovery(graph)

	b.ResetTimer()

	for b.Loop() {
		discovery.Learn(2, 0.05)
	}
}
