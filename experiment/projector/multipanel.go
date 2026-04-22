package projector

import (
	"io"
	"os"
)

// MPSeries is one data series in a chart (non-heatmap) panel.
type MPSeries struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`     // "line" | "bar" | "dashed" | "dotted"
	Symbol   string    `json:"symbol"`   // "none" | "circle" | "diamond" | "triangle"
	BarWidth string    `json:"barWidth"` // legacy; ignored by matplotlib renderer
	Area     bool      `json:"area"`     // fill area under a line series
	Data     []float64 `json:"data"`
	Color    string    `json:"color"` // optional fixed hex color
}

// MPPanel describes one sub-chart panel in the figure.
type MPPanel struct {
	Kind string `json:"kind"` // "heatmap" | "chart"

	Title string `json:"title"` // panel title shown above the grid

	// Legacy ECharts grid hints — kept on the struct so callers don't break,
	// but the matplotlib backend lays panels out on its own.
	GridLeft   string `json:"gridLeft,omitempty"`
	GridRight  string `json:"gridRight,omitempty"`
	GridTop    string `json:"gridTop,omitempty"`
	GridBottom string `json:"gridBottom,omitempty"`

	// Shared axes
	XLabels   []string `json:"x_labels"`
	XAxisName string   `json:"x_axis_name"`
	XInterval int      `json:"x_interval,omitempty"`
	XShow     bool     `json:"x_show"`

	// Heatmap-specific
	YLabels     []string `json:"y_labels,omitempty"`
	YAxisName   string   `json:"y_axis_name,omitempty"`
	YInterval   int      `json:"y_interval,omitempty"`
	HeatData    [][]any  `json:"heat_data,omitempty"`
	HeatMin     float64  `json:"heat_min,omitempty"`
	HeatMax     float64  `json:"heat_max,omitempty"`
	ColorScheme string   `json:"cmap,omitempty"`
	ShowVM      bool     `json:"show_vm,omitempty"` // legacy ECharts visual-map flag
	VMRight     string   `json:"vm_right,omitempty"`

	// Chart-specific
	Series []MPSeries `json:"series,omitempty"`
	YMin   *float64   `json:"y_min,omitempty"`
	YMax   *float64   `json:"y_max,omitempty"`
}

// MultiPanel renders N panels (heatmap / line / bar / combo) into one PDF.
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

	// Legacy ECharts knobs retained on the struct so the existing
	// adapters (and any external callers) compile unchanged. The
	// matplotlib backend ignores them.
	tooltipTrigger      string
	tooltipPosition     string
	tooltipPositionSet  bool
	legendTop           string
	legendRight         string
	legendRightExplicit bool
	legendSelectedMode  *bool
}

type multiPanelOpts func(*MultiPanel)

func NewMultiPanel(opts ...multiPanelOpts) *MultiPanel {
	selectedMode := false
	mp := &MultiPanel{
		out:                os.Stdout,
		filename:           "multipanel",
		outDir:             ".",
		width:              1200,
		height:             900,
		legendSelectedMode: &selectedMode,
	}
	for _, opt := range opts {
		opt(mp)
	}
	return mp
}

func (mp *MultiPanel) SetOutput(out io.Writer) { mp.out = out }

/*
Generate emits the figure PDF (via the matplotlib renderer) and the
LaTeX figure stub. The PDF is named filename.pdf inside outDir.
*/
func (mp *MultiPanel) Generate() error {
	spec := struct {
		Title  string    `json:"title"`
		Width  int       `json:"width"`
		Height int       `json:"height"`
		Panels []MPPanel `json:"panels"`
	}{
		Title:  mp.title,
		Width:  mp.width,
		Height: mp.height,
		Panels: mp.panels,
	}

	if err := runPython("multipanel", spec, mp.outDir, mp.filename); err != nil {
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

// F64 wraps a float64 as a pointer for MPPanel.YMin / YMax (nil = auto).
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
