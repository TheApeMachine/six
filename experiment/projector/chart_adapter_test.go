package projector

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBarChartAsMultiPanel(t *testing.T) {
	Convey("Given a bar chart with one series", t, func() {
		chart := NewBarChart(
			BarChartWithAxes(
				[]string{"alpha", "beta"},
				[]BarSeries{{Name: "score", Data: []float64{1, 2}}},
			),
			BarChartWithMeta("Bar", "Caption", "fig:bar"),
			BarChartWithOutput("paper/figures", "bar_chart"),
		)

		multiPanel := chart.asMultiPanel()

		Convey("It should translate to a single-panel multipanel figure", func() {
			So(multiPanel.title, ShouldEqual, "Bar")
			So(multiPanel.caption, ShouldEqual, "Caption")
			So(multiPanel.label, ShouldEqual, "fig:bar")
			So(multiPanel.outDir, ShouldEqual, "paper/figures")
			So(multiPanel.filename, ShouldEqual, "bar_chart")
			So(multiPanel.width, ShouldEqual, chartW)
			So(multiPanel.height, ShouldEqual, chartH)
			So(multiPanel.tooltipTrigger, ShouldEqual, "axis")
			So(multiPanel.legendTop, ShouldEqual, "5%")
			So(multiPanel.legendSelectedMode, ShouldBeNil)
			So(len(multiPanel.panels), ShouldEqual, 1)

			panel := multiPanel.panels[0]
			So(panel.Kind, ShouldEqual, "chart")
			So(panel.GridLeft, ShouldEqual, "10%")
			So(panel.GridRight, ShouldEqual, "10%")
			So(panel.GridTop, ShouldEqual, "20%")
			So(panel.GridBottom, ShouldEqual, "15%")
			So(panel.XLabels, ShouldResemble, []string{"alpha", "beta"})
			So(panel.XShow, ShouldBeTrue)
			So(len(panel.Series), ShouldEqual, 1)
			So(panel.Series[0], ShouldResemble, MPSeries{Name: "score", Kind: "bar", BarWidth: "auto", Data: []float64{1, 2}})
		})
	})
}

func BenchmarkBarChartAsMultiPanel(b *testing.B) {
	chart := NewBarChart(
		BarChartWithAxes(
			[]string{"alpha", "beta", "gamma"},
			[]BarSeries{{Name: "score", Data: []float64{1, 2, 3}}},
		),
		BarChartWithMeta("Bar", "Caption", "fig:bar"),
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = chart.asMultiPanel()
	}
}

func TestLineChartAsMultiPanel(t *testing.T) {
	Convey("Given a line chart with an explicit range", t, func() {
		chart := NewLineChart(
			LineChartWithAxes(
				[]string{"epoch-1", "epoch-2"},
				[]LineSeries{{Name: "loss", Data: []float64{0.6, 0.4}}},
			),
			LineChartWithMeta("Line", "Caption", "fig:line"),
			LineChartWithOutput("paper/figures", "line_chart"),
			LineChartWithYRange(0.1, 0.9),
		)

		multiPanel := chart.asMultiPanel()

		Convey("It should preserve the line defaults inside one chart panel", func() {
			So(multiPanel.tooltipTrigger, ShouldEqual, "axis")
			So(multiPanel.legendTop, ShouldEqual, "5%")
			So(len(multiPanel.panels), ShouldEqual, 1)

			panel := multiPanel.panels[0]
			So(panel.GridLeft, ShouldEqual, "10%")
			So(panel.GridRight, ShouldEqual, "10%")
			So(panel.GridTop, ShouldEqual, "20%")
			So(panel.GridBottom, ShouldEqual, "15%")
			So(panel.YMin, ShouldNotBeNil)
			So(panel.YMax, ShouldNotBeNil)
			So(*panel.YMin, ShouldEqual, 0.1)
			So(*panel.YMax, ShouldEqual, 0.9)
			So(panel.Series[0], ShouldResemble, MPSeries{
				Name:   "loss",
				Kind:   "line",
				Symbol: "none",
				Data:   []float64{0.6, 0.4},
			})
		})
	})
}

func BenchmarkLineChartAsMultiPanel(b *testing.B) {
	chart := NewLineChart(
		LineChartWithAxes(
			[]string{"epoch-1", "epoch-2", "epoch-3"},
			[]LineSeries{{Name: "loss", Data: []float64{0.6, 0.4, 0.2}}},
		),
		LineChartWithMeta("Line", "Caption", "fig:line"),
		LineChartWithYRange(0.0, 1.0),
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = chart.asMultiPanel()
	}
}

func TestComboChartAsMultiPanel(t *testing.T) {
	Convey("Given a combo chart with mixed series kinds", t, func() {
		chart := NewComboChart(
			ComboChartWithAxes(
				[]string{"step-1", "step-2"},
				[]ComboSeries{
					{Name: "bars", Type: "bar", Data: []float64{3, 4}},
					{Name: "trend", Type: "line", Data: []float64{0.2, 0.5}},
					{Name: "baseline", Type: "dashed", Data: []float64{0.3, 0.3}},
				},
			),
			ComboChartWithAxisLabels("Steps", "Score"),
			ComboChartWithMeta("Combo", "Caption", "fig:combo"),
			ComboChartWithOutput("paper/figures", "combo_chart"),
			ComboChartWithYRange(0, 1),
		)

		multiPanel := chart.asMultiPanel()

		Convey("It should keep combo-specific styling when translated", func() {
			So(multiPanel.tooltipTrigger, ShouldEqual, "axis")
			So(multiPanel.legendTop, ShouldEqual, "3%")
			So(multiPanel.legendSelectedMode, ShouldNotBeNil)
			So(*multiPanel.legendSelectedMode, ShouldBeFalse)
			So(len(multiPanel.panels), ShouldEqual, 1)

			panel := multiPanel.panels[0]
			So(panel.GridLeft, ShouldEqual, "10%")
			So(panel.GridRight, ShouldEqual, "10%")
			So(panel.GridTop, ShouldEqual, "18%")
			So(panel.GridBottom, ShouldEqual, "15%")
			So(panel.XAxisName, ShouldEqual, "Steps")
			So(panel.YAxisName, ShouldEqual, "Score")
			So(panel.YMin, ShouldNotBeNil)
			So(panel.YMax, ShouldNotBeNil)
			So(*panel.YMin, ShouldEqual, 0.0)
			So(*panel.YMax, ShouldEqual, 1.0)
			So(panel.Series, ShouldResemble, []MPSeries{
				{Name: "bars", Kind: "bar", BarWidth: "15%", Data: []float64{3, 4}},
				{Name: "trend", Kind: "line", Symbol: "circle", Data: []float64{0.2, 0.5}},
				{Name: "baseline", Kind: "dashed", Symbol: "diamond", Data: []float64{0.3, 0.3}},
			})
		})
	})
}

func BenchmarkComboChartAsMultiPanel(b *testing.B) {
	chart := NewComboChart(
		ComboChartWithAxes(
			[]string{"step-1", "step-2", "step-3"},
			[]ComboSeries{
				{Name: "bars", Type: "bar", Data: []float64{3, 4, 5}},
				{Name: "trend", Type: "line", Data: []float64{0.2, 0.5, 0.7}},
				{Name: "baseline", Type: "dashed", Data: []float64{0.3, 0.3, 0.3}},
			},
		),
		ComboChartWithAxisLabels("Steps", "Score"),
		ComboChartWithMeta("Combo", "Caption", "fig:combo"),
		ComboChartWithYRange(0, 1),
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = chart.asMultiPanel()
	}
}

func TestMultiPanelScriptSpec(t *testing.T) {
	Convey("Given a multipanel figure using the public constructor", t, func() {
		multiPanel := NewMultiPanel(
			MultiPanelWithPanels(ChartPanel([]string{"alpha"}, []MPSeries{{Name: "bars", Kind: "bar", Data: []float64{1}}}, nil, nil)),
		)

		spec := multiPanel.scriptSpec()

		Convey("It should preserve the historical non-interactive legend default", func() {
			So(spec.Legend.SelectedMode, ShouldNotBeNil)
			So(*spec.Legend.SelectedMode, ShouldBeFalse)
		})
	})
}
