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

//go:embed heatmap_script.tmpl
var heatmapScriptTmpl string

type HeatMap struct {
	out       io.Writer
	title     string
	xAxisData []string
	yAxisData []string
	data      [][]any
	min       float64
	max       float64
	caption   string
	label     string
	filename  string
	outDir    string
}

type heatMapOpts func(*HeatMap)

func NewHeatMap(opts ...heatMapOpts) *HeatMap {
	hm := &HeatMap{
		out:      os.Stdout,
		filename: "heatmap",
		outDir:   ".",
		min:      0,
		max:      1,
	}
	for _, opt := range opts {
		opt(hm)
	}
	return hm
}

func (hm *HeatMap) SetOutput(out io.Writer) {
	hm.out = out
}

func (hm *HeatMap) Generate() error {
	scriptTmpl, err := template.New("heatmap_script").Parse(heatmapScriptTmpl)
	if err != nil {
		return err
	}

	xData, _ := json.Marshal(hm.xAxisData)
	yData, _ := json.Marshal(hm.yAxisData)
	hData, _ := json.Marshal(hm.data)

	scriptData := struct {
		XAxisDataJSON string
		YAxisDataJSON string
		DataJSON      string
		Min           float64
		Max           float64
	}{
		XAxisDataJSON: string(xData),
		YAxisDataJSON: string(yData),
		DataJSON:      string(hData),
		Min:           hm.min,
		Max:           hm.max,
	}

	var scriptBuf bytes.Buffer
	if err := scriptTmpl.Execute(&scriptBuf, scriptData); err != nil {
		return err
	}

	html, err := renderChartHTML(hm.title, 1200, 800, scriptBuf.String())
	if err != nil {
		return err
	}

	if err := os.MkdirAll(hm.outDir, 0755); err != nil {
		return err
	}

	htmlPath := filepath.Join(hm.outDir, hm.filename+".html")
	pdfPath := filepath.Join(hm.outDir, hm.filename+".pdf")

	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		return err
	}

	// 2. Headless Chrome Render to PDF
	if err := ExportPDF(htmlPath, pdfPath); err != nil {
		return err
	}

	// 3. Emit LaTeX Figure Wrapper
	figTmpl, err := template.New("figure").Parse(figureTmpl)
	if err != nil {
		return err
	}

	figData := struct {
		Filename string
		Caption  string
		Label    string
	}{
		Filename: fmt.Sprintf("%s.pdf", hm.filename),
		Caption:  hm.caption,
		Label:    hm.label,
	}

	var figBuf bytes.Buffer
	if err := figTmpl.Execute(&figBuf, figData); err != nil {
		return err
	}

	_, err = hm.out.Write(figBuf.Bytes())
	return err
}

func HeatMapWithData(xAxis, yAxis []string, data [][]any, min, max float64) heatMapOpts {
	return func(hm *HeatMap) {
		hm.xAxisData = xAxis
		hm.yAxisData = yAxis
		hm.data = data
		hm.min = min
		hm.max = max
	}
}

func HeatMapWithMeta(title, caption, label string) heatMapOpts {
	return func(hm *HeatMap) {
		hm.title = title
		hm.caption = caption
		hm.label = label
	}
}

func HeatMapWithOutput(outDir, filename string) heatMapOpts {
	return func(hm *HeatMap) {
		hm.outDir = outDir
		hm.filename = filename
	}
}
