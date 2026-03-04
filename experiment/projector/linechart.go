package projector

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed linechart_script.tmpl
var linechartScriptTmpl string

type LineSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

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
	c := &LineChart{
		out:      os.Stdout,
		filename: "linechart",
		outDir:   ".",
		yMin:     0,
		yMax:     1,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (chart *LineChart) SetOutput(out io.Writer) {
	chart.out = out
}

func (chart *LineChart) Generate() error {
	scriptTmpl, err := template.New("linechart_script").Parse(linechartScriptTmpl)
	if err != nil {
		return err
	}

	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)

	scriptData := struct {
		XAxisDataJSON string
		SeriesJSON    string
		YMin          float64
		YMax          float64
	}{
		XAxisDataJSON: string(xData),
		SeriesJSON:    string(sData),
		YMin:          chart.yMin,
		YMax:          chart.yMax,
	}

	var scriptBuf bytes.Buffer
	if err := scriptTmpl.Execute(&scriptBuf, scriptData); err != nil {
		return err
	}

	html, err := renderChartHTML(chart.title, 1200, 800, scriptBuf.String())
	if err != nil {
		return err
	}

	if err := os.MkdirAll(chart.outDir, 0755); err != nil {
		return err
	}

	htmlPath := filepath.Join(chart.outDir, chart.filename+".html")
	pdfPath := filepath.Join(chart.outDir, chart.filename+".pdf")

	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		return err
	}

	if err := ExportPDF(htmlPath, pdfPath); err != nil {
		return err
	}

	figTmpl, err := template.New("figure").Parse(figureTmpl)
	if err != nil {
		return err
	}

	figData := struct {
		Filename string
		Caption  string
		Label    string
	}{
		Filename: fmt.Sprintf("%s.pdf", chart.filename),
		Caption:  chart.caption,
		Label:    chart.label,
	}

	var figBuf bytes.Buffer
	if err := figTmpl.Execute(&figBuf, figData); err != nil {
		return err
	}

	_, err = chart.out.Write(figBuf.Bytes())
	return err
}

func LineChartWithAxes(xAxis []string, series []LineSeries) lineChartOpts {
	return func(c *LineChart) {
		c.xAxisData = xAxis
		c.series = series
	}
}

func LineChartWithMeta(title, caption, label string) lineChartOpts {
	return func(c *LineChart) {
		c.title = title
		c.caption = caption
		c.label = label
	}
}

func LineChartWithOutput(outDir, filename string) lineChartOpts {
	return func(c *LineChart) {
		c.outDir = outDir
		c.filename = filename
	}
}

func LineChartWithYRange(yMin, yMax float64) lineChartOpts {
	return func(c *LineChart) {
		c.yMin = yMin
		c.yMax = yMax
	}
}
