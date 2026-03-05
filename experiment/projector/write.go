package projector

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Write functions accept an explicit outDir so callers don't need to repeat
// the ensurePaperDir + projector construction boilerplate.

func WriteTable(data []map[string]any, outDir, outFile string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(outDir + "/" + outFile)
	if err != nil {
		return err
	}
	defer f.Close()
	defer TriggerAutoBuild()
	return NewTable(TableWithData(data), TableWithOutput(f)).Generate()
}

var autobuildOnce sync.Once

// TriggerAutoBuild spawns a background bash script to debounce and compile the paper automatically.
func TriggerAutoBuild() {
	autobuildOnce.Do(func() {
		wd, _ := os.Getwd()
		rootDir := ""
		for d := wd; d != ""; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
				rootDir = d
				break
			}
			if d == filepath.Dir(d) {
				break
			}
		}
		if rootDir == "" {
			return
		}

		script := `
sleep 1
if mkdir /tmp/six_paper_lock 2>/dev/null; then
	trap 'rm -rf /tmp/six_paper_lock' EXIT
	sleep 3
	cd "` + rootDir + `" || exit 1
	# don't trigger go run paper inside an active go build lock if we can avoid it
	go run main.go paper >/dev/null 2>&1
	cd paper || exit 1
	pdflatex -interaction=nonstopmode main.tex >/dev/null 2>&1
fi
`
		cmd := exec.Command("bash", "-c", script)
		cmd.Start()
	})
}

func WriteBarChart(xAxis []string, series []BarSeries, title, caption, label, outDir, filename string, out *os.File) error {
	defer TriggerAutoBuild()
	c := NewBarChart(
		BarChartWithAxes(xAxis, series),
		BarChartWithMeta(title, caption, label),
		BarChartWithOutput(outDir, filename),
	)
	if out != nil {
		c.SetOutput(out)
	}
	return c.GenerateToDisk()
}

func WriteLineChart(xAxis []string, series []LineSeries, title, caption, label, outDir, filename string, yMin, yMax float64, out *os.File) error {
	defer TriggerAutoBuild()
	c := NewLineChart(
		LineChartWithAxes(xAxis, series),
		LineChartWithMeta(title, caption, label),
		LineChartWithOutput(outDir, filename),
		LineChartWithYRange(yMin, yMax),
	)
	if out != nil {
		c.SetOutput(out)
	}
	return c.Generate()
}

func WriteComboChart(xAxis []string, series []ComboSeries, xName, yName string, yMin, yMax float64, title, caption, label, outDir, filename string, out *os.File) error {
	defer TriggerAutoBuild()
	c := NewComboChart(
		ComboChartWithAxes(xAxis, series),
		ComboChartWithAxisLabels(xName, yName),
		ComboChartWithMeta(title, caption, label),
		ComboChartWithOutput(outDir, filename),
		ComboChartWithYRange(yMin, yMax),
	)
	if out != nil {
		c.SetOutput(out)
	}
	return c.Generate()
}

func WriteHeatMap(xAxis, yAxis []string, data [][]any, minV, maxV float64, title, caption, label, outDir, filename string, out *os.File) error {
	defer TriggerAutoBuild()
	hm := NewHeatMap(
		HeatMapWithData(xAxis, yAxis, data, minV, maxV),
		HeatMapWithMeta(title, caption, label),
		HeatMapWithOutput(outDir, filename),
	)
	if out != nil {
		hm.SetOutput(out)
	}
	return hm.Generate()
}

func WriteMultiPanel(panels []MPPanel, width, height int, title, caption, label, outDir, filename string, out *os.File) error {
	defer TriggerAutoBuild()
	mp := NewMultiPanel(
		MultiPanelWithPanels(panels...),
		MultiPanelWithMeta(title, caption, label),
		MultiPanelWithOutput(outDir, filename),
		MultiPanelWithSize(width, height),
	)
	if out != nil {
		mp.SetOutput(out)
	}
	return mp.Generate()
}

func WriteProse(tmplSrc string, data map[string]any, outDir, outFile string) error {
	defer TriggerAutoBuild()
	p := NewProse(
		ProseWithTemplate(tmplSrc),
		ProseWithData(data),
		ProseWithOutput(outDir, outFile),
	)
	p.SetOutput(io.Discard)
	return p.Generate()
}

func WriteImageStrip(rows []ImageStripRow, title, caption, label, outDir, filename string, out *os.File) error {
	defer TriggerAutoBuild()
	is := NewImageStrip(
		ImageStripWithData(rows),
		ImageStripWithMeta(title, caption, label),
		ImageStripWithOutput(outDir, filename),
	)
	if out != nil {
		is.SetOutput(out)
	}
	return is.Generate()
}
