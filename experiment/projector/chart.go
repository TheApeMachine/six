package projector

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	texttmpl "text/template"
)

//go:embed chart_layout.tmpl
var chartLayoutTmpl string

// renderChartHTML composes the shared HTML layout with chart-specific JS.
// ChartScript is passed as template.JS so html/template does not HTML-escape
// JavaScript code inside <script> tags.
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
func renderAndExport(html, outDir, filename string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	htmlPath := filepath.Join(outDir, filename+".html")
	if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
		return err
	}
	return ExportPDF(htmlPath, filepath.Join(outDir, filename+".pdf"))
}

// emitFigure renders the shared LaTeX \begin{figure}…\end{figure} wrapper to out.
func emitFigure(filename, caption, label string, out io.Writer) error {
	var buf bytes.Buffer
	if err := texttmpl.Must(texttmpl.New("fig").Parse(figureTmpl)).Execute(&buf, struct {
		Filename string
		Caption  string
		Label    string
	}{fmt.Sprintf("%s.pdf", filename), caption, label}); err != nil {
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
