package pattern

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExtract(t *testing.T) {
	t.Parallel()

	Convey("Extract returns scored symbols above thresholds", t, func() {
		root := any("root")
		child := any("child")

		visitor := NodeVisitor{
			SortedChildren: func(nodeID any) []string {
				if nodeID == root {
					return []string{"a"}
				}

				return nil
			},
			Child: func(nodeID any, token string) any {
				if nodeID == root && token == "a" {
					return child
				}

				return nil
			},
			EffectiveCount: func(nodeID any, label string) float64 {
				if nodeID != child {
					return 0
				}

				if label == "L1" {
					return 10
				}

				if label == "L2" {
					return 2
				}

				return 0
			},
		}

		/*
			Combined mass for "a" is 12 (L1=10, L2=2), so minTotal 5 passes. The L2-only
			row is still scored; minScore filters out that weak arm so only the L1 symbol
			remains.
		*/
		symbols := Extract(root, []string{"L1", "L2"}, visitor, 5, 0.25, 10)

		So(len(symbols), ShouldEqual, 1)
		So(symbols[0].Symbol, ShouldEqual, "a")
		So(symbols[0].Label, ShouldEqual, "L1")

		expected := 10.0 / 12.0 * math.Log1p(10) * math.Sqrt(1)

		So(symbols[0].Score, ShouldAlmostEqual, expected, 1e-9)

		Convey("with minScore 11 nothing clears the bar", func() {
			out := Extract(root, []string{"L1", "L2"}, visitor, 5, 11, 10)

			So(out, ShouldHaveLength, 0)
		})
	})
}

func BenchmarkExtract(b *testing.B) {
	root := any("root")
	leaf := any("leaf")

	visitor := NodeVisitor{
		SortedChildren: func(nodeID any) []string {
			if nodeID == root {
				return []string{"a"}
			}

			return nil
		},
		Child: func(nodeID any, token string) any {
			if nodeID == root && token == "a" {
				return leaf
			}

			return nil
		},
		EffectiveCount: func(nodeID any, _ string) float64 {
			if nodeID == leaf {
				return 8
			}

			return 0
		},
	}

	labels := []string{"L"}

	b.ResetTimer()

	for b.Loop() {
		_ = Extract(root, labels, visitor, 1, 0, 50)
	}
}
