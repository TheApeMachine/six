package projector

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
)

type ProseEntry struct {
	Condition   func() bool
	Description string
}

// Prose renders a LaTeX prose snippet (subsection, paragraph, table prose, etc.)
// by executing a Go text/template against a data map of real experiment values.
// The template uses standard Go template syntax:  {{.Ceiling | printf "%.4f"}}
type Prose struct {
	tmplSrc string
	data    map[string]any
	outDir  string
	outFile string
	out     io.Writer
}

type proseOpts func(*Prose)

func NewProse(opts ...proseOpts) *Prose {
	p := &Prose{
		out: os.Stdout,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Prose) SetOutput(out io.Writer) { p.out = out }

func (p *Prose) Generate() error {
	funcMap := template.FuncMap{
		// Format float with N decimal places
		"f2": func(v float64) string { return formatF(v, 2) },
		"f3": func(v float64) string { return formatF(v, 3) },
		"f4": func(v float64) string { return formatF(v, 4) },
		// Percentage: multiplies by 100 and appends \% (LaTeX-safe)
		"pct": func(v float64) string {
			return formatF(v*100, 1) + `\%`
		},
		// Sign-prefixed float (+0.0087 / -0.0012)
		"signed": func(v float64) string {
			if v >= 0 {
				return "+" + formatF(v, 4)
			}
			return formatF(v, 4)
		},
		// Escape an arbitrary string for safe embedding in LaTeX
		"latex": LaTeXEscape,
		"esc":   LaTeXEscape, // short alias
	}

	tmpl, err := template.New("prose").Funcs(funcMap).Parse(p.tmplSrc)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p.data); err != nil {
		return err
	}

	// Write to file if outDir+outFile are set
	if p.outDir != "" && p.outFile != "" {
		if err := os.MkdirAll(p.outDir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(p.outDir, p.outFile), buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	// Also write to the configured io.Writer (for chaining with Section)
	_, err = p.out.Write(buf.Bytes())
	return err
}

// ─── Option functions ──────────────────────────────────────────────────────────

func ProseWithTemplate(src string) proseOpts {
	return func(p *Prose) { p.tmplSrc = src }
}

func ProseWithData(data map[string]any) proseOpts {
	return func(p *Prose) { p.data = data }
}

func ProseWithOutput(outDir, outFile string) proseOpts {
	return func(p *Prose) {
		p.outDir = outDir
		p.outFile = outFile
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func formatF(v float64, prec int) string {
	switch prec {
	case 2:
		return fmt2(v)
	case 3:
		return fmt3(v)
	default:
		return fmt4(v)
	}
}

func fmt2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func fmt3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
func fmt4(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
