package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed barchart_script.tmpl
var barchartScriptTmpl string

// BarSeries is one named data series in a bar chart.
type BarSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

// BarChart renders an ECharts bar chart to HTML+PDF and emits a LaTeX figure stub.
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

func (chart *BarChart) Generate() error {
	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)
	script := execTemplate(barchartScriptTmpl, struct {
		XAxisDataJSON string
		SeriesJSON    string
	}{string(xData), string(sData)})
	html, err := renderChartHTML(chart.title, 1200, 600, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, chart.outDir, chart.filename); err != nil {
		return err
	}
	return emitFigure(chart.filename, chart.caption, chart.label, chart.out)
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
