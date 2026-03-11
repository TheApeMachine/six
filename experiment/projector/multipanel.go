package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed multipanel_script.tmpl
var multipanelScriptTmpl string

// MPSeries is one data series in a chart (non-heatmap) panel.
type MPSeries struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`     // "line" | "bar" | "dashed" | "dotted"
	Symbol   string    `json:"symbol"`   // "none" | "circle" | "diamond" | "triangle"
	BarWidth string    `json:"barWidth"` // e.g. "20%"
	Area     bool      `json:"area"`     // fill area under a line series
	Data     []float64 `json:"data"`
	Color    string    `json:"color"` // optional fixed hex color
}

// MPPanel describes one sub-chart panel in the figure.
type MPPanel struct {
	Kind string `json:"kind"` // "heatmap" | "chart"

	Title string `json:"title"` // panel title shown above the grid

	// ECharts grid position — percent strings e.g. "5%"
	GridLeft   string `json:"gridLeft"`
	GridRight  string `json:"gridRight"`
	GridTop    string `json:"gridTop"`
	GridBottom string `json:"gridBottom"`

	// Shared axes
	XLabels   []string `json:"xLabels"`
	XAxisName string   `json:"xAxisName"`
	XInterval int      `json:"xInterval"` // 0 = auto
	XShow     bool     `json:"xShow"`

	// Heatmap-specific
	YLabels     []string `json:"yLabels"`
	YAxisName   string   `json:"yAxisName"`
	YInterval   int      `json:"yInterval"`
	HeatData    [][]any  `json:"heatData"` // [[xi, yi, value], …]
	HeatMin     float64  `json:"heatMin"`
	HeatMax     float64  `json:"heatMax"`
	ColorScheme string   `json:"colorScheme"` // "viridis" | "magma" | "plasma"
	ShowVM      bool     `json:"showVM"`      // show the visual-map legend bar
	VMRight     string   `json:"vmRight"`     // anchor e.g. "44%"

	// Chart-specific
	Series []MPSeries `json:"series"`
	YMin   *float64   `json:"yMin"` // nil → ECharts auto
	YMax   *float64   `json:"yMax"`
}

// MultiPanel renders N panels (heatmap / line / bar / combo) into one ECharts figure.
type MultiPanel struct {
	out      io.Writer
	title    string
	panels   []MPPanel
	caption  string
	label    string
	filename string
	outDir   string
	width    int
	height   int
}

type multiPanelOpts func(*MultiPanel)

func NewMultiPanel(opts ...multiPanelOpts) *MultiPanel {
	mp := &MultiPanel{out: os.Stdout, filename: "multipanel", outDir: ".", width: 1200, height: 900}
	for _, opt := range opts {
		opt(mp)
	}
	return mp
}

func (mp *MultiPanel) SetOutput(out io.Writer) { mp.out = out }

func (mp *MultiPanel) Generate() error {
	panelsJSON, err := json.Marshal(mp.panels)
	if err != nil {
		return err
	}
	script := execTemplate(multipanelScriptTmpl, struct{ PanelsJSON string }{string(panelsJSON)})
	html, err := renderChartHTML(mp.title, mp.width, mp.height, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, mp.outDir, mp.filename, mp.width, mp.height); err != nil {
		return err
	}
	return emitFigure(mp.filename, mp.caption, mp.label, mp.out)
}

// ─── Option functions ───────────────────────────────────────────────────────

func MultiPanelWithPanels(panels ...MPPanel) multiPanelOpts {
	return func(mp *MultiPanel) { mp.panels = panels }
}

func MultiPanelWithMeta(title, caption, label string) multiPanelOpts {
	return func(mp *MultiPanel) { mp.title = title; mp.caption = caption; mp.label = label }
}

func MultiPanelWithOutput(outDir, filename string) multiPanelOpts {
	return func(mp *MultiPanel) { mp.outDir = outDir; mp.filename = filename }
}

func MultiPanelWithSize(width, height int) multiPanelOpts {
	return func(mp *MultiPanel) { mp.width = width; mp.height = height }
}

// ─── Convenience constructors ───────────────────────────────────────────────

// F64 wraps a float64 as a pointer for MPPanel.YMin / YMax (nil = ECharts auto).
func F64(v float64) *float64 { return &v }

// HeatmapPanel returns an MPPanel pre-configured as a heatmap.
func HeatmapPanel(xLabels, yLabels []string, data [][]any, heatMin, heatMax float64, cs string) MPPanel {
	return MPPanel{
		Kind: "heatmap", XLabels: xLabels, YLabels: yLabels,
		HeatData: data, HeatMin: heatMin, HeatMax: heatMax,
		ColorScheme: cs, ShowVM: true, XShow: true,
	}
}

// ChartPanel returns an MPPanel pre-configured as a line/bar chart.
func ChartPanel(xLabels []string, series []MPSeries, yMin, yMax *float64) MPPanel {
	return MPPanel{Kind: "chart", XLabels: xLabels, Series: series, YMin: yMin, YMax: yMax, XShow: true}
}
