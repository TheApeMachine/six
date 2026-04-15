package projector

import (
	"io"
	"os"
)

// ComboSeries describes one series in a combo (mixed bar+line) chart.
// Kind is one of "bar", "line" (smooth), or "dashed".
type ComboSeries struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`     // "bar" | "line" | "dashed"
	Symbol   string    `json:"symbol"`   // e.g. "diamond", "triangle"
	BarWidth string    `json:"barWidth"` // e.g. "15%"
	Data     []float64 `json:"data"`
}

// ComboChart generates an ECharts figure with mixed series types.
type ComboChart struct {
	out       io.Writer
	title     string
	xAxisData []string
	xAxisName string
	yAxisName string
	series    []ComboSeries
	caption   string
	label     string
	filename  string
	outDir    string
	yMin      float64
	yMax      float64
}

type comboChartOpts func(*ComboChart)

func NewComboChart(opts ...comboChartOpts) *ComboChart {
	c := &ComboChart{out: os.Stdout, filename: "combochart", outDir: ".", yMax: 1}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (chart *ComboChart) SetOutput(out io.Writer) { chart.out = out }

func (chart *ComboChart) Generate() error {
	return chart.asMultiPanel().Generate()
}

func ComboChartWithAxes(xAxis []string, series []ComboSeries) comboChartOpts {
	return func(c *ComboChart) { c.xAxisData = xAxis; c.series = series }
}

func ComboChartWithAxisLabels(xName, yName string) comboChartOpts {
	return func(c *ComboChart) { c.xAxisName = xName; c.yAxisName = yName }
}

func ComboChartWithMeta(title, caption, label string) comboChartOpts {
	return func(c *ComboChart) { c.title = title; c.caption = caption; c.label = label }
}

func ComboChartWithOutput(outDir, filename string) comboChartOpts {
	return func(c *ComboChart) { c.outDir = outDir; c.filename = filename }
}

func ComboChartWithYRange(yMin, yMax float64) comboChartOpts {
	return func(c *ComboChart) { c.yMin = yMin; c.yMax = yMax }
}
