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

// renderAndExport writes html to outDir/filename.html and produces a PDF alongside it.
// width/height are the chart pixel dimensions used for the headless browser viewport.
func renderAndExport(html string, outDir, filename string, dims ...int) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	htmlPath := filepath.Join(outDir, filename+".html")
	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		return err
	}
	w, h := 0, 0
	switch len(dims) {
	case 0:
	case 2:
		w, h = dims[0], dims[1]
	default:
		return fmt.Errorf("projector.renderAndExport(%s): invalid dims %v; expected 0 or 2 values (width,height)", htmlPath, dims)
	}
	return ExportPDFWithSize(htmlPath, filepath.Join(outDir, filename+".pdf"), w, h)
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
