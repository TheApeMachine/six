package scaling

import (
	"io"
	"os"
	"path/filepath"

	"github.com/theapemachine/six/experiment/projector"
)

var scalingPaperDirMemo string

func paperDir() string {
	if scalingPaperDirMemo != "" {
		return scalingPaperDirMemo
	}
	if d := os.Getenv("SIX_PAPER_DIR"); d != "" {
		scalingPaperDirMemo = d
		return scalingPaperDirMemo
	}
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for dir := wd; dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			scalingPaperDirMemo = filepath.Join(dir, "paper", "include", "scaling")
			return scalingPaperDirMemo
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	scalingPaperDirMemo = filepath.Join(wd, "paper", "include", "scaling")
	return scalingPaperDirMemo
}

func ensurePaperDir() (string, error) {
	dir := paperDir()
	return dir, os.MkdirAll(dir, 0755)
}

func writeScalingTable(data []map[string]any, outFile string) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	return projector.WriteTable(data, dir, outFile)
}

func writeMultiPanel(panels []projector.MPPanel, width, height int, title, caption, label, filename string, out *os.File) error {
	dir, err := ensurePaperDir()
	if err != nil {
		return err
	}
	return projector.WriteMultiPanel(panels, width, height, title, caption, label, dir, filename, out)
}

func writeProse(tmplSrc string, data map[string]any, outFile string) error {
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
