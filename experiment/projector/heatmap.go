package projector

import (
	"io"
	"os"
)

// HeatMap renders a heatmap PDF and emits a LaTeX figure stub.
type HeatMap struct {
	out       io.Writer
	title     string
	xAxisData []string
	yAxisData []string
	data      [][]any
	min       float64
	max       float64
	cmap      string
	caption   string
	label     string
	filename  string
	outDir    string
}

type heatMapOpts func(*HeatMap)

func NewHeatMap(opts ...heatMapOpts) *HeatMap {
	hm := &HeatMap{out: os.Stdout, filename: "heatmap", outDir: ".", max: 1, cmap: "viridis"}
	for _, opt := range opts {
		opt(hm)
	}
	return hm
}

func (hm *HeatMap) SetOutput(out io.Writer) { hm.out = out }

func (hm *HeatMap) Generate() error {
	spec := struct {
		Title string   `json:"title"`
		XAxis []string `json:"x_axis"`
		YAxis []string `json:"y_axis"`
		Data  [][]any  `json:"data"`
		VMin  float64  `json:"v_min"`
		VMax  float64  `json:"v_max"`
		CMap  string   `json:"cmap"`
	}{hm.title, hm.xAxisData, hm.yAxisData, hm.data, hm.min, hm.max, hm.cmap}

	if err := runPython("heatmap", spec, hm.outDir, hm.filename); err != nil {
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

func HeatMapWithColorScheme(cmap string) heatMapOpts {
	return func(hm *HeatMap) { hm.cmap = cmap }
}
