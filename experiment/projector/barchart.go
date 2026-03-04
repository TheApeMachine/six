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

//go:embed barchart_script.tmpl
var barchartScriptTmpl string

type BarSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

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
	c := &BarChart{
		out:      os.Stdout,
		filename: "barchart",
		outDir:   ".",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (chart *BarChart) SetOutput(out io.Writer) {
	chart.out = out
}

func (chart *BarChart) Generate() error {
	scriptTmpl, err := template.New("barchart_script").Parse(barchartScriptTmpl)
	if err != nil {
		return err
	}

	xData, _ := json.Marshal(chart.xAxisData)
	sData, _ := json.Marshal(chart.series)

	scriptData := struct {
		XAxisDataJSON string
		SeriesJSON    string
	}{
		XAxisDataJSON: string(xData),
		SeriesJSON:    string(sData),
	}

	var scriptBuf bytes.Buffer
	if err := scriptTmpl.Execute(&scriptBuf, scriptData); err != nil {
		return err
	}

	html, err := renderChartHTML(chart.title, 1200, 600, scriptBuf.String())
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

	// 2. Headless Chrome Render to PDF
	if err := ExportPDF(htmlPath, pdfPath); err != nil {
		return err
	}

	// 3. Emit LaTeX Figure Wrapper to output stream
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

func BarChartWithAxes(xAxis []string, series []BarSeries) barChartOpts {
	return func(c *BarChart) {
		c.xAxisData = xAxis
		c.series = series
	}
}

func BarChartWithMeta(title, caption, label string) barChartOpts {
	return func(c *BarChart) {
		c.title = title
		c.caption = caption
		c.label = label
	}
}

func BarChartWithOutput(outDir, filename string) barChartOpts {
	return func(c *BarChart) {
		c.outDir = outDir
		c.filename = filename
	}
}
