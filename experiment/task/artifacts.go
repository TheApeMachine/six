package task

import (
	"io"
	"os"
	"path/filepath"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
)

var paperDirMemo = make(map[string]string)

// PaperDir returns the output directory for paper artifacts.
// If section is provided, it uses that subdirectory under paper/include/.
// Defaults to "codegen" for backward compatibility.
func PaperDir(section ...string) string {
	sec := "codegen"
	if len(section) > 0 && section[0] != "" {
		sec = section[0]
	}

	if d, ok := paperDirMemo[sec]; ok {
		return d
	}

	if d := os.Getenv("SIX_PAPER_DIR"); d != "" {
		result := filepath.Join(d, sec)
		paperDirMemo[sec] = result
		return result
	}

	wd, err := os.Getwd()
	if err != nil {
		panic("failed to get working directory: " + err.Error())
	}

	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			result := filepath.Join(dir, "paper", "include", sec)
			paperDirMemo[sec] = result
			return result
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}

	result := filepath.Join(wd, "paper", "include", sec)
	paperDirMemo[sec] = result
	return result
}

func ensurePaperDir(section ...string) (string, error) {
	dir := PaperDir(section...)
	return dir, os.MkdirAll(dir, 0755)
}

func WriteTable(data []tools.ExperimentalData, outFile string, section ...string) error {
	dir, err := ensurePaperDir(section...)

	if err != nil {
		return err
	}

	return projector.WriteTable(data, dir, outFile)
}

func WriteBarChart(xAxis []string, series []projector.BarSeries, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)

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

func WriteLineChart(xAxis []string, series []projector.LineSeries, title, caption, label, filename string, yMin, yMax float64, section ...string) error {
	dir, err := ensurePaperDir(section...)

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

func WriteComboChart(xAxis []string, series []projector.ComboSeries, xName, yName string, yMin, yMax float64, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
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

func WriteHeatMap(xAxis, yAxis []string, data [][]any, minV, maxV float64, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
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

func WriteConfusionMatrix(labels []string, matrix [][]int, meanScore float64, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteConfusionMatrix(labels, matrix, meanScore, title, caption, label, dir, filename, f)
}

func WriteMultiPanel(panels []projector.MPPanel, width, height int, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
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

func WriteProse(tmplSrc string, data map[string]any, outFile string, section ...string) error {
	dir, err := ensurePaperDir(section...)
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

func WriteCodeAppendix(sections []projector.CodeSection, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}
	return projector.WriteCodeAppendix(sections, dir, filename)
}
