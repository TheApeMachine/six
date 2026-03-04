package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed heatmap_script.tmpl
var heatmapScriptTmpl string

// HeatMap renders an ECharts heatmap to HTML+PDF and emits a LaTeX figure stub.
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
	hm := &HeatMap{out: os.Stdout, filename: "heatmap", outDir: ".", max: 1}
	for _, opt := range opts {
		opt(hm)
	}
	return hm
}

func (hm *HeatMap) SetOutput(out io.Writer) { hm.out = out }

func (hm *HeatMap) Generate() error {
	xData, _ := json.Marshal(hm.xAxisData)
	yData, _ := json.Marshal(hm.yAxisData)
	hData, _ := json.Marshal(hm.data)
	script := execTemplate(heatmapScriptTmpl, struct {
		XAxisDataJSON string
		YAxisDataJSON string
		DataJSON      string
		Min           float64
		Max           float64
	}{string(xData), string(yData), string(hData), hm.min, hm.max})
	html, err := renderChartHTML(hm.title, 1200, 800, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, hm.outDir, hm.filename); err != nil {
		return err
	}
	return emitFigure(hm.filename, hm.caption, hm.label, hm.out)
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
	return func(hm *HeatMap) { hm.title = title; hm.caption = caption; hm.label = label }
}

func HeatMapWithOutput(outDir, filename string) heatMapOpts {
	return func(hm *HeatMap) { hm.outDir = outDir; hm.filename = filename }
}
