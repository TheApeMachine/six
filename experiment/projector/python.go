package projector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

/*
Figure rendering is delegated to a small matplotlib pipeline under
scripts/figures/. The Go side serializes the chart spec to JSON, pipes
it into `python3 render.py <out_path>`, and the script writes a PDF
directly to outDir/filename.pdf. SIX_STRICT_PDF=1 makes Python failures
fail-fast; otherwise we warn (matching the previous chromedp/ECharts
behaviour so callers don't break when matplotlib isn't installed).

If SIX_FIGURE_PYTHON is set in the environment, that path is used as
the Python interpreter (handy for venvs); otherwise we look for
`python3` then `python` in PATH.

The renderer script directory is resolved by walking up from the
current working directory until we find a `go.mod`, then joining
`scripts/figures`. SIX_FIGURE_SCRIPTS overrides the discovery.
*/

const figureRendererScript = "render.py"

var (
	pythonOnce sync.Once
	pythonExe  string
	pythonErr  error

	scriptsOnce sync.Once
	scriptsDir  string
	scriptsErr  error
)

func resolvePython() (string, error) {
	pythonOnce.Do(func() {
		if env := os.Getenv("SIX_FIGURE_PYTHON"); env != "" {
			pythonExe = env
			return
		}
		for _, name := range []string{"python3", "python"} {
			if p, err := exec.LookPath(name); err == nil {
				pythonExe = p
				return
			}
		}
		pythonErr = fmt.Errorf("projector: neither python3 nor python found in PATH (set SIX_FIGURE_PYTHON to override)")
	})
	return pythonExe, pythonErr
}

func resolveScriptsDir() (string, error) {
	scriptsOnce.Do(func() {
		if env := os.Getenv("SIX_FIGURE_SCRIPTS"); env != "" {
			scriptsDir = env
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			scriptsErr = fmt.Errorf("projector: get working directory: %w", err)
			return
		}
		for dir := wd; dir != ""; dir = filepath.Dir(dir) {
			if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
				scriptsDir = filepath.Join(dir, "scripts", "figures")
				return
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
		// Fallback: assume scripts/figures sits next to wherever the
		// test was invoked from.
		scriptsDir = filepath.Join(wd, "scripts", "figures")
	})
	return scriptsDir, scriptsErr
}

/*
runPython invokes the matplotlib renderer for one figure. kind is the
top-level dispatcher key in render.py; spec is the per-kind payload
(any JSON-marshalable value). The PDF lands at outDir/filename.pdf.
*/
func runPython(kind string, spec any, outDir, filename string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("projector.runPython: mkdir %s: %w", outDir, err)
	}

	exe, err := resolvePython()
	if err != nil {
		if StrictPDFExport() {
			return err
		}
		fmt.Fprintf(os.Stderr, "Warning: %v (figure %s skipped)\n", err, filename)
		return nil
	}

	dir, err := resolveScriptsDir()
	if err != nil {
		if StrictPDFExport() {
			return err
		}
		fmt.Fprintf(os.Stderr, "Warning: %v (figure %s skipped)\n", err, filename)
		return nil
	}

	scriptPath := filepath.Join(dir, figureRendererScript)
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		if StrictPDFExport() {
			return fmt.Errorf("projector.runPython: render script missing at %s: %w", scriptPath, statErr)
		}
		fmt.Fprintf(os.Stderr, "Warning: render script missing at %s: %v (figure %s skipped)\n", scriptPath, statErr, filename)
		return nil
	}

	envelope := struct {
		Kind string `json:"kind"`
		Spec any    `json:"spec"`
	}{Kind: kind, Spec: spec}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("projector.runPython: marshal spec for %s: %w", filename, err)
	}

	pdfPath := filepath.Join(outDir, filename+".pdf")

	cmd := exec.Command(exe, scriptPath, pdfPath)
	cmd.Stdin = bytes.NewReader(payload)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard

	// Inherit env so MPLCONFIGDIR / SIX_FIGURE_PYTHON / etc. flow through.
	cmd.Env = append(os.Environ(),
		"PYTHONUNBUFFERED=1",
		"MPLBACKEND=pdf",
	)

	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("projector.runPython: %s render for %s failed (%s): %v\n%s",
			kind, filename, runtime.GOOS, err, stderr.String())
		if StrictPDFExport() {
			return fmt.Errorf("%s", msg)
		}
		fmt.Fprintln(os.Stderr, "Warning: "+msg)
	}

	return nil
}
