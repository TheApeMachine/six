package vision

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
			paperDirMemo = filepath.Join(dir, "paper", "include", "vision")
			return paperDirMemo
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	paperDirMemo = filepath.Join(wd, "paper", "include", "vision")
	return paperDirMemo
}

func ensurePaperDir() (string, error) {
	dir := PaperDir()
	return dir, os.MkdirAll(dir, 0755)
}

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
