package task

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
)

var paperDirMemo = make(map[string]string)
var paperDirMemoMu sync.RWMutex

// PaperDir returns the output directory for paper artifacts.
// If section is provided, it uses that subdirectory under paper/include/.
// Defaults to "codegen" for backward compatibility.
func PaperDir(section ...string) string {
	sec := "codegen"
	if len(section) > 0 && section[0] != "" {
		sec = section[0]
	}

	paperDirMemoMu.RLock()
	d, ok := paperDirMemo[sec]
	paperDirMemoMu.RUnlock()
	if ok {
		return d
	}

	if d := os.Getenv("SIX_PAPER_DIR"); d != "" {
		result := filepath.Join(d, sec)
		paperDirMemoMu.Lock()
		paperDirMemo[sec] = result
		paperDirMemoMu.Unlock()
		return result
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to get working directory: %v\n", err)
		os.Exit(1)
	}

	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			result := filepath.Join(dir, "paper", "include", sec)
			paperDirMemoMu.Lock()
			paperDirMemo[sec] = result
			paperDirMemoMu.Unlock()
			return result
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}

	result := filepath.Join(wd, "paper", "include", sec)
	paperDirMemoMu.Lock()
	paperDirMemo[sec] = result
	paperDirMemoMu.Unlock()
	return result
}

func ensurePaperDir(section ...string) (string, error) {
	dir := PaperDir(section...)
	return dir, os.MkdirAll(dir, 0755)
}

func WriteTable(data any, outFile string, section ...string) error {
	dir, err := ensurePaperDir(section...)

	if err != nil {
		return err
	}

	return projector.WriteTable(data, dir, outFile)
}

func WriteStandardSummary(
	name, section string,
	rows []tools.ExperimentalData,
	holdoutN int,
	holdoutType string,
	timing runTiming,
) error {
	dir, err := ensurePaperDir(section)
	if err != nil {
		return err
	}

	outFile := tools.Slugify(name) + "_summary.tex"
	return projector.WriteSummaryTable(
		name, section, rows,
		holdoutN, holdoutType,
		projector.DefaultSummaryConfig(),
		projector.RunTiming{
			LoadDur:     timing.loadDur,
			PromptDur:   timing.promptDur,
			FinalizeDur: timing.finalizeDur,
			N:           timing.n,
		},
		dir, outFile,
	)
}

func WriteJSONFile(data any, outFile string, section ...string) error {
	dir, err := ensurePaperDir(section...)

	if err != nil {
		return err
	}

	payload, err := json.MarshalIndent(data, "", "  ")

	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, outFile), payload, 0644)
}

func WriteBarChart(xAxis []string, series []tools.BarSeries, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)

	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, filename+".tex"))

	if err != nil {
		return err
	}

	defer f.Close()

	return projector.WriteBarChart(xAxis, projectorBarSeries(series), title, caption, label, dir, filename, f)
}

func WriteLineChart(xAxis []string, series []tools.LineSeries, title, caption, label, filename string, yMin, yMax float64, section ...string) error {
	dir, err := ensurePaperDir(section...)

	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, filename+".tex"))

	if err != nil {
		return err
	}

	defer f.Close()

	return projector.WriteLineChart(xAxis, projectorLineSeries(series), title, caption, label, dir, filename, yMin, yMax, f)
}

func WriteComboChart(xAxis []string, series []tools.ComboSeries, xName, yName string, yMin, yMax float64, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteComboChart(xAxis, projectorComboSeries(series), xName, yName, yMin, yMax, title, caption, label, dir, filename, f)
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

func WriteMultiPanel(panels []tools.Panel, width, height int, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}
	defer f.Close()
	return projector.WriteMultiPanel(projectorPanels(panels), width, height, title, caption, label, dir, filename, f)
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

func WriteImageStrip(rows []tools.ImageStripRow, title, caption, label, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}

	defer f.Close()

	return projector.WriteImageStrip(projectorImageStripRows(rows), title, caption, label, dir, filename, f)
}

func WritePolarConstraint(data projector.PolarConstraintData, filename string, section ...string) error {
	dir, err := ensurePaperDir(section...)
	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, filename+".tex"))
	if err != nil {
		return err
	}

	defer f.Close()

	return projector.WritePolarConstraint(data, dir, filename, f)
}

func projectorBarSeries(series []tools.BarSeries) []projector.BarSeries {
	out := make([]projector.BarSeries, len(series))
	for i, s := range series {
		out[i] = projector.BarSeries{
			Name: s.Name,
			Data: s.Data,
		}
	}
	return out
}

func projectorLineSeries(series []tools.LineSeries) []projector.LineSeries {
	out := make([]projector.LineSeries, len(series))
	for i, s := range series {
		out[i] = projector.LineSeries{
			Name: s.Name,
			Data: s.Data,
		}
	}
	return out
}

func projectorComboSeries(series []tools.ComboSeries) []projector.ComboSeries {
	out := make([]projector.ComboSeries, len(series))
	for i, s := range series {
		out[i] = projector.ComboSeries{
			Name:     s.Name,
			Type:     s.Type,
			Symbol:   s.Symbol,
			BarWidth: s.BarWidth,
			Data:     s.Data,
		}
	}
	return out
}

func projectorPanels(panels []tools.Panel) []projector.MPPanel {
	out := make([]projector.MPPanel, len(panels))
	for i, p := range panels {
		out[i] = projector.MPPanel{
			Kind:        p.Kind,
			Title:       p.Title,
			GridLeft:    p.GridLeft,
			GridRight:   p.GridRight,
			GridTop:     p.GridTop,
			GridBottom:  p.GridBottom,
			XLabels:     p.XLabels,
			XAxisName:   p.XAxisName,
			XInterval:   p.XInterval,
			XShow:       p.XShow,
			YLabels:     p.YLabels,
			YAxisName:   p.YAxisName,
			YInterval:   p.YInterval,
			HeatData:    p.HeatData,
			HeatMin:     p.HeatMin,
			HeatMax:     p.HeatMax,
			ColorScheme: p.ColorScheme,
			ShowVM:      p.ShowVM,
			VMRight:     p.VMRight,
			Series:      projectorPanelSeries(p.Series),
			YMin:        p.YMin,
			YMax:        p.YMax,
		}
	}
	return out
}

func projectorPanelSeries(series []tools.PanelSeries) []projector.MPSeries {
	out := make([]projector.MPSeries, len(series))
	for i, s := range series {
		out[i] = projector.MPSeries{
			Name:     s.Name,
			Kind:     s.Kind,
			Symbol:   s.Symbol,
			BarWidth: s.BarWidth,
			Area:     s.Area,
			Data:     s.Data,
			Color:    s.Color,
		}
	}
	return out
}

func projectorImageStripRows(rows []tools.ImageStripRow) []projector.ImageStripRow {
	out := make([]projector.ImageStripRow, len(rows))
	for i, row := range rows {
		out[i] = projector.ImageStripRow{
			Original:      row.Original,
			Masked:        row.Masked,
			Reconstructed: row.Reconstructed,
			Label:         row.Label,
		}
	}
	return out
}
