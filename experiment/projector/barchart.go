package projector

import (
	"io"
	"os"
)

// BarSeries is one named data series in a bar chart.
type BarSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

// BarChart renders a grouped bar PDF and emits a LaTeX figure stub.
type BarChart struct {
	out       io.Writer
	title     string
	xAxisData []string
	series    []BarSeries
	caption   string
	label     string
	filename  string
	outDir    string
}

type barChartOpts func(*BarChart)

func NewBarChart(opts ...barChartOpts) *BarChart {
	c := &BarChart{out: os.Stdout, filename: "barchart", outDir: "."}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (chart *BarChart) SetOutput(out io.Writer) { chart.out = out }

// Generate writes the PDF and the LaTeX figure stub.
func (chart *BarChart) Generate() error {
	return chart.asMultiPanel().Generate()
}

func BarChartWithAxes(xAxis []string, series []BarSeries) barChartOpts {
	return func(c *BarChart) { c.xAxisData = xAxis; c.series = series }
}

func BarChartWithMeta(title, caption, label string) barChartOpts {
	return func(c *BarChart) { c.title = title; c.caption = caption; c.label = label }
}

func BarChartWithOutput(outDir, filename string) barChartOpts {
	return func(c *BarChart) { c.outDir = outDir; c.filename = filename }
}
