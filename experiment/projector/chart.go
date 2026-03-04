package projector

import (
	"bytes"
	_ "embed"
	"html/template"
)

//go:embed chart_layout.tmpl
var chartLayoutTmpl string

// renderChartHTML composes the shared HTML layout with chart-specific script content.
// ChartScript is passed as template.JS so it is not escaped (html/template would
// JavaScript-escape strings inside <script>, breaking the code).
func renderChartHTML(title string, width, height int, chartScript string) (string, error) {
	tmpl, err := template.New("layout").Parse(chartLayoutTmpl)
	if err != nil {
		return "", err
	}
	data := struct {
		Title       string
		Width       int
		Height      int
		ChartScript template.JS
	}{
		Title:       title,
		Width:       width,
		Height:      height,
		ChartScript: template.JS(chartScript),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
