package trialmap

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
)

func TestTwoScorePanels(t *testing.T) {
	t.Parallel()

	Convey("TwoScorePanels returns nil when rows empty", t, func() {
		So(TwoScorePanels(nil, 0, StandardTwoPanel(), nil), ShouldBeNil)
	})

	Convey("Given one row", t, func() {
		rows := []tools.ExperimentalData{
			{
				Scores: tools.Scores{
					Exact: 1, Partial: 0.5, Fuzzy: 0.25,
				},
				WeightedTotal: 0.7,
			},
		}

		Convey("StandardTwoPanel uses S1 and four heat cells", func() {
			panels := TwoScorePanels(rows, 0.55, StandardTwoPanel(), nil)

			So(len(panels), ShouldEqual, 2)
			So(panels[0].YLabels, ShouldResemble, []string{"S1"})
			So(len(panels[0].HeatData), ShouldEqual, 4)
			So(panels[1].Series[0].Data[0], ShouldEqual, 0.7)
		})

		Convey("BabiTwoPanel uses Q1 label", func() {
			panels := TwoScorePanels(rows, 0.55, BabiTwoPanel(), nil)

			So(panels[0].YLabels, ShouldResemble, []string{"Q1"})
			So(panels[0].XAxisName, ShouldEqual, "Score Dimension")
		})

		Convey("custom sampleLabels are preserved", func() {
			panels := TwoScorePanels(rows, 0.1, StandardTwoPanel(), []string{"dom.1"})

			So(panels[0].YLabels, ShouldResemble, []string{"dom.1"})
		})
	})
}

func BenchmarkTwoScorePanels(b *testing.B) {
	rows := make([]tools.ExperimentalData, 64)

	for idx := range rows {
		rows[idx] = tools.ExperimentalData{
			Scores: tools.Scores{
				Exact:   float64(idx%5) * 0.1,
				Partial: float64(idx%7) * 0.05,
				Fuzzy:   float64(idx%3) * 0.1,
			},
			WeightedTotal: float64(idx%10) * 0.05,
		}
	}

	grids := StandardTwoPanel()

	b.ResetTimer()

	for b.Loop() {
		_ = TwoScorePanels(rows, 0.33, grids, nil)
	}
}
