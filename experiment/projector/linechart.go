package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed linechart_script.tmpl
var linechartScriptTmpl string

// LineSeries is one named data series in a line chart.
type LineSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

// LineChart renders an ECharts line chart to HTML+PDF and emits a LaTeX figure stub.
type LineChart struct {
	out       io.Writer
	title     string
	xAxisData []string
	series    []LineSeries
	caption   string
	label     string
	filename  string
	outDir    string
	yMin      float64
	yMax      float64
}

type lineChartOpts func(*LineChart)

func NewLineChart(opts ...lineChartOpts) *LineChart {
	c := &LineChart{out: os.Stdout, filename: "linechart", outDir: ".", yMax: 1}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (chart *LineChart) SetOutput(out io.Writer) { chart.out = out }

func (chart *LineChart) Generate() error {
	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)
	script := execTemplate(linechartScriptTmpl, struct {
		XAxisDataJSON string
		SeriesJSON    string
		YMin          float64
		YMax          float64
	}{string(xData), string(sData), chart.yMin, chart.yMax})
	html, err := renderChartHTML(chart.title, 1200, 800, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, chart.outDir, chart.filename); err != nil {
		return err
	}
	return emitFigure(chart.filename, chart.caption, chart.label, chart.out)
}

func LineChartWithAxes(xAxis []string, series []LineSeries) lineChartOpts {
	return func(c *LineChart) { c.xAxisData = xAxis; c.series = series }
}

func LineChartWithMeta(title, caption, label string) lineChartOpts {
	return func(c *LineChart) { c.title = title; c.caption = caption; c.label = label }
}

func LineChartWithOutput(outDir, filename string) lineChartOpts {
	return func(c *LineChart) { c.outDir = outDir; c.filename = filename }
}

func LineChartWithYRange(yMin, yMax float64) lineChartOpts {
	return func(c *LineChart) { c.yMin = yMin; c.yMax = yMax }
}
