package codegen

import (
	"io"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/experiment/projector"
)

var paperDirMemo string

func PaperDir() string {
	if paperDirMemo != "" {
		return paperDirMemo
	}
	if d := os.Getenv("SIX_PAPER_DIR"); d != "" {
		paperDirMemo = d
		return paperDirMemo
	}
	wd, _ := os.Getwd()
	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			paperDirMemo = filepath.Join(dir, "paper", "include", "codegen")
			return paperDirMemo
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	paperDirMemo = filepath.Join(wd, "paper", "include", "codegen")
	return paperDirMemo
}

func ensurePaperDir() (string, error) {
	dir := PaperDir()
	return dir, os.MkdirAll(dir, 0755)
}

// ─── Thin wrappers — all logic lives in projector.Write* ──────────────────

func WriteTable(data []map[string]any, outFile string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	return projector.WriteTable(data, dir, outFile)
}

func WriteBarChart(xAxis []string, series []projector.BarSeries, title, caption, label, filename string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteBarChart(xAxis, series, title, caption, label, dir, filename, f)
}

func WriteLineChart(xAxis []string, series []projector.LineSeries, title, caption, label, filename string, yMin, yMax float64) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteLineChart(xAxis, series, title, caption, label, dir, filename, yMin, yMax, f)
}

func WriteComboChart(xAxis []string, series []projector.ComboSeries, xName, yName string, yMin, yMax float64, title, caption, label, filename string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteComboChart(xAxis, series, xName, yName, yMin, yMax, title, caption, label, dir, filename, f)
}

func WriteHeatMap(xAxis, yAxis []string, data [][]any, minV, maxV float64, title, caption, label, filename string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteHeatMap(xAxis, yAxis, data, minV, maxV, title, caption, label, dir, filename, f)
}

func WriteMultiPanel(panels []projector.MPPanel, width, height int, title, caption, label, filename string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteMultiPanel(panels, width, height, title, caption, label, dir, filename, f)
}

func WriteProse(tmplSrc string, data map[string]any, outFile string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	p := projector.NewProse(
		projector.ProseWithTemplate(tmplSrc),
		projector.ProseWithData(data),
		projector.ProseWithOutput(dir, outFile),
	)
	p.SetOutput(io.Discard)
	return p.Generate()
}

func WriteCodeAppendix(sections []projector.CodeSection, filename string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	return projector.WriteCodeAppendix(sections, dir, filename)
}
