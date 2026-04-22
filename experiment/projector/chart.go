package projector

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	texttmpl "text/template"
)

// chartHTMLTempPrefix is the basename prefix for tempfile HTML used only as
// Chrome input during PDF export; it is removed afterward and never published.
const chartHTMLTempPrefix = "six-chart-"

//go:embed chart_layout.tmpl
var chartLayoutTmpl string

// Default chart pixel dimensions.  All projectors share these so the
// headless-browser viewport, ECharts canvas, and PDF paper size agree.
const (
	chartW = 1200
	chartH = 800
)

// renderChartHTML composes the shared HTML layout with chart-specific JS.
// Width and Height set the explicit pixel dimensions for the chart container,
// the CSS @page rule, and the html/body elements — ensuring the headless
// browser viewport, ECharts canvas, and PDF output all agree on size.
func renderChartHTML(title string, width, height int, chartScript string) (string, error) {
	tmpl, err := template.New("layout").Parse(chartLayoutTmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Title       string
		Width       int
		Height      int
		ChartScript template.JS
	}{title, width, height, template.JS(chartScript)}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderAndExport writes chart HTML to a tempfile, produces filename.pdf in outDir,
// then deletes the HTML. Chrome still needs a file:// document; the paper tree keeps
// only TeX stubs, JSON snapshots, and PDFs.
func renderAndExport(html string, outDir, filename string, dims ...int) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", chartHTMLTempPrefix+"*.html")
	if err != nil {
		return fmt.Errorf("projector.renderAndExport: create temp html: %w", err)
	}

	htmlPath := tmp.Name()

	defer func() {
		_ = os.Remove(htmlPath)
	}()

	if _, err := tmp.Write([]byte(html)); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("projector.renderAndExport: write temp html: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("projector.renderAndExport: close temp html: %w", err)
	}

	w, h := 0, 0

	switch len(dims) {
	case 0:
	case 2:
		w, h = dims[0], dims[1]
	default:
		return fmt.Errorf("projector.renderAndExport(%s): invalid dims %v; expected 0 or 2 values (width,height)", filename, dims)
	}

	pdfPath := filepath.Join(outDir, filename+".pdf")

	if err := ExportPDFWithSize(htmlPath, pdfPath, w, h); err != nil {
		if os.Getenv("SIX_STRICT_PDF") == "1" {
			return fmt.Errorf("export PDF for %s: %w", pdfPath, err)
		}

		fmt.Fprintf(os.Stderr, "Warning: failed to export PDF (is Chrome installed?): %v\n", err)
	}

	return nil
}

/*
StrictPDFExport reports whether SIX_STRICT_PDF is set (fail chart export when
Chrome/headless PDF is unavailable). Callers above the projector layer can
surface this in help text or config validation.
*/
func StrictPDFExport() bool {
	return os.Getenv("SIX_STRICT_PDF") == "1"
}

// emitFigure renders the shared LaTeX \begin{figure}…\end{figure} wrapper to out.
func emitFigure(filename, caption, label string, out io.Writer) error {
	var buf bytes.Buffer
	if err := texttmpl.Must(texttmpl.New("fig").Parse(figureTmpl)).Execute(&buf, struct {
		Filename string
		Caption  string
		Label    string
	}{fmt.Sprintf("%s.pdf", filename), strings.ReplaceAll(caption, "%", `\%`), label}); err != nil {
		return err
	}
	_, err := out.Write(buf.Bytes())
	return err
}

// execTemplate renders a text/template source string against data.
// Returns empty string on execution error (parse errors panic via Must).
func execTemplate(src string, data any) string {
	var buf bytes.Buffer
	if err := texttmpl.Must(texttmpl.New("").Parse(src)).Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}

