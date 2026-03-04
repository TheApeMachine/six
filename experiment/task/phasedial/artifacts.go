package phasedial

import (
	"os"
	"path/filepath"

	"github.com/theapemachine/six/experiment/projector"
)

// paperDirMemo caches the resolved paper directory.
var paperDirMemo string

// PaperDir returns the path to paper/include/phasedial. Uses SIX_PAPER_DIR
// if set, otherwise paper/include/phasedial relative to repo root (directory
// containing go.mod). Falls back to ./paper/include/phasedial from cwd.
func PaperDir() string {
	if paperDirMemo != "" {
		return paperDirMemo
	}
	if d := os.Getenv("SIX_PAPER_DIR"); d != "" {
		paperDirMemo = d
		return paperDirMemo
	}
	wd, _ := os.Getwd()
	// Walk up to find go.mod
	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			paperDirMemo = filepath.Join(dir, "paper", "include", "phasedial")
			return paperDirMemo
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	paperDirMemo = filepath.Join(wd, "paper", "include", "phasedial")
	return paperDirMemo
}

// WriteSection writes a projector Section to the paper directory.
func WriteSection(title, content string, elements ...projector.Interface) error {
	dir := PaperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "phasedial.tex"))
	if err != nil {
		return err
	}
	defer f.Close()

	section := projector.NewSection(
		projector.SectionWithTitle(title),
		projector.SectionWithContent(content),
		projector.SectionWithElements(elements...),
		projector.SectionWithOutput(f),
	)
	return section.Generate()
}

// WriteTable writes a projector Table to the given file in the paper directory.
func WriteTable(data []map[string]any, outPath string) error {
	dir := PaperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, outPath))
	if err != nil {
		return err
	}
	defer f.Close()

	table := projector.NewTable(
		projector.TableWithData(data),
		projector.TableWithOutput(f),
	)
	return table.Generate()
}

// WriteBarChart creates a BarChart, writes HTML+PDF to PaperDir, and LaTeX to out.
func WriteBarChart(xAxis []string, series []projector.BarSeries, title, caption, label, filename string, out *os.File) error {
	dir := PaperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	chart := projector.NewBarChart(
		projector.BarChartWithAxes(xAxis, series),
		projector.BarChartWithMeta(title, caption, label),
		projector.BarChartWithOutput(dir, filename),
	)
	if out != nil {
		chart.SetOutput(out)
	}
	return chart.Generate()
}

// WriteLineChart creates a LineChart, writes HTML+PDF to PaperDir, and LaTeX to out.
func WriteLineChart(xAxis []string, series []projector.LineSeries, title, caption, label, filename string, yMin, yMax float64, out *os.File) error {
	dir := PaperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	chart := projector.NewLineChart(
		projector.LineChartWithAxes(xAxis, series),
		projector.LineChartWithMeta(title, caption, label),
		projector.LineChartWithOutput(dir, filename),
		projector.LineChartWithYRange(yMin, yMax),
	)
	if out != nil {
		chart.SetOutput(out)
	}
	return chart.Generate()
}

// WriteHeatMap creates a HeatMap, writes HTML+PDF to PaperDir, and LaTeX to out.
func WriteHeatMap(xAxis, yAxis []string, data [][]any, minV, maxV float64, title, caption, label, filename string, out *os.File) error {
	dir := PaperDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	hm := projector.NewHeatMap(
		projector.HeatMapWithData(xAxis, yAxis, data, minV, maxV),
		projector.HeatMapWithMeta(title, caption, label),
		projector.HeatMapWithOutput(dir, filename),
	)
	if out != nil {
		hm.SetOutput(out)
	}
	return hm.Generate()
}
