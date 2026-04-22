package projector

import "io"

/*
singlePanelChartLayout captures the shared single-canvas layout used by the
legacy bar, line, and combo chart projectors.
*/
type singlePanelChartLayout struct {
	gridTop    string
	legendTop  string
	rightUnset bool
}

func (chart *BarChart) asMultiPanel() *MultiPanel {
	panel := MPPanel{
		Kind:       "chart",
		GridLeft:   "10%",
		GridRight:  "10%",
		GridTop:    "20%",
		GridBottom: "15%",
		XLabels:    chart.xAxisData,
		XShow:      true,
		Series:     barSeriesToMP(chart.series),
	}

	return newSinglePanelMultiPanel(
		chart.out,
		chart.title,
		chart.caption,
		chart.label,
		chart.outDir,
		chart.filename,
		panel,
		singlePanelChartLayout{gridTop: "20%", legendTop: "5%", rightUnset: true},
	)
}

func (chart *LineChart) asMultiPanel() *MultiPanel {
	panel := MPPanel{
		Kind:       "chart",
		GridLeft:   "10%",
		GridRight:  "10%",
		GridTop:    "20%",
		GridBottom: "15%",
		XLabels:    chart.xAxisData,
		XShow:      true,
		Series:     lineSeriesToMP(chart.series),
		YMin:       F64(chart.yMin),
		YMax:       F64(chart.yMax),
	}

	return newSinglePanelMultiPanel(
		chart.out,
		chart.title,
		chart.caption,
		chart.label,
		chart.outDir,
		chart.filename,
		panel,
		singlePanelChartLayout{gridTop: "20%", legendTop: "5%", rightUnset: true},
	)
}

func (chart *ComboChart) asMultiPanel() *MultiPanel {
	selectedMode := false
	panel := MPPanel{
		Kind:       "chart",
		GridLeft:   "10%",
		GridRight:  "10%",
		GridTop:    "18%",
		GridBottom: "15%",
		XLabels:    chart.xAxisData,
		XAxisName:  chart.xAxisName,
		YAxisName:  chart.yAxisName,
		XShow:      true,
		Series:     comboSeriesToMP(chart.series),
		YMin:       F64(chart.yMin),
		YMax:       F64(chart.yMax),
	}

	multiPanel := newSinglePanelMultiPanel(
		chart.out,
		chart.title,
		chart.caption,
		chart.label,
		chart.outDir,
		chart.filename,
		panel,
		singlePanelChartLayout{gridTop: "18%", legendTop: "3%", rightUnset: true},
	)
	multiPanel.legendSelectedMode = &selectedMode

	return multiPanel
}

func newSinglePanelMultiPanel(
	out io.Writer,
	title, caption, label, outDir, filename string,
	panel MPPanel,
	layout singlePanelChartLayout,
) *MultiPanel {
	return &MultiPanel{
		out:                 out,
		title:               title,
		panels:              []MPPanel{panel},
		caption:             caption,
		label:               label,
		filename:            filename,
		outDir:              outDir,
		width:               chartW,
		height:              chartH,
		tooltipTrigger:      "axis",
		legendTop:           layout.legendTop,
		legendRightExplicit: layout.rightUnset,
	}
}

func barSeriesToMP(series []BarSeries) []MPSeries {
	projected := make([]MPSeries, 0, len(series))

	for _, item := range series {
		projected = append(projected, MPSeries{
			Name:     item.Name,
			Kind:     "bar",
			BarWidth: "auto",
			Data:     item.Data,
		})
	}

	return projected
}

func lineSeriesToMP(series []LineSeries) []MPSeries {
	projected := make([]MPSeries, 0, len(series))

	for _, item := range series {
		projected = append(projected, MPSeries{
			Name:   item.Name,
			Kind:   "line",
			Symbol: "none",
			Data:   item.Data,
		})
	}

	return projected
}

func comboSeriesToMP(series []ComboSeries) []MPSeries {
	projected := make([]MPSeries, 0, len(series))

	for _, item := range series {
		projected = append(projected, MPSeries{
			Name:     item.Name,
			Kind:     comboSeriesKind(item.Type),
			Symbol:   comboSeriesSymbol(item.Type, item.Symbol),
			BarWidth: comboSeriesBarWidth(item.Type, item.BarWidth),
			Data:     item.Data,
		})
	}

	return projected
}

func comboSeriesKind(kind string) string {
	if kind == "bar" {
		return "bar"
	}

	if kind == "dashed" {
		return "dashed"
	}

	return "line"
}

func comboSeriesSymbol(kind, symbol string) string {
	if symbol != "" {
		return symbol
	}

	if kind == "dashed" {
		return "diamond"
	}

	if kind == "bar" {
		return ""
	}

	return "circle"
}

func comboSeriesBarWidth(kind, width string) string {
	if kind != "bar" {
		return width
	}

	if width != "" {
		return width
	}

	return "15%"
}

