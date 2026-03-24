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

func (chart *BarChart) RenderHTML(w io.Writer) error {
	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)
	script := execTemplate(barchartScriptTmpl, struct {
		XAxisDataJSON string
		SeriesJSON    string
	}{string(xData), string(sData)})
	html, err := renderChartHTML(chart.title, chartW, chartH, script)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(html))
	return err
}

func (chart *BarChart) RenderLaTeX(w io.Writer) error {
	return emitFigure(chart.filename, chart.caption, chart.label, w)
}

func (chart *BarChart) GenerateToDisk() error {
	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)
	script := execTemplate(barchartScriptTmpl, struct {
		XAxisDataJSON string
		SeriesJSON    string
	}{string(xData), string(sData)})
	html, err := renderChartHTML(chart.title, chartW, chartH, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, chart.outDir, chart.filename, chartW, chartH); err != nil {
		return err
	}
	return chart.RenderLaTeX(chart.out)
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
