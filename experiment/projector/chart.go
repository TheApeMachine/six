package projector

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"
	texttmpl "text/template"
)

// Default chart pixel dimensions. The matplotlib renderer treats these as
// CSS pixels at 96 DPI when sizing figures.
const (
	chartW = 1200
	chartH = 800
)

//go:embed figure.tmpl
var figureTmpl string

/*
StrictPDFExport reports whether SIX_STRICT_PDF is set. When enabled,
projector failures (missing python, missing matplotlib, render errors)
return errors instead of warning-and-continuing.
*/
func StrictPDFExport() bool {
	return os.Getenv("SIX_STRICT_PDF") == "1"
}

// emitFigure renders the shared LaTeX \begin{figure}…\end{figure} wrapper to out.
func emitFigure(filename, caption, label string, out io.Writer) error {
	if out == nil {
		return nil
	}
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
