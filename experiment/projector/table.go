package projector

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"sort"
	"text/template"

	tools "github.com/theapemachine/six/experiment"
)

//go:embed table.tmpl
var tableTmpl string

type Table struct {
	headers []string
	rows    [][]any
	out     io.Writer
}

type tableOpts func(*Table)

func NewTable(opts ...tableOpts) *Table {
	t := &Table{
		out: os.Stdout,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (table *Table) Generate() error {
	tmpl, err := template.New("table").Parse(tableTmpl)
	if err != nil {
		return err
	}

	templateData := struct {
		Headers []string
		Rows    [][]any
	}{
		Headers: table.headers,
		Rows:    table.rows,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return err
	}

	_, err = table.out.Write(buf.Bytes())
	return err
}

func TableWithData(data any) tableOpts {
	return func(table *Table) {
		switch v := data.(type) {
		case []tools.ExperimentalData:
			table.headers = []string{"Idx", "Name", "Exact", "Partial", "Fuzzy", "Total"}
			for _, d := range v {
				table.rows = append(table.rows, []any{
					d.Idx,
					LaTeXEscape(d.Name),
					fmt.Sprintf("%.4f", d.Scores.Exact),
					fmt.Sprintf("%.4f", d.Scores.Partial),
					fmt.Sprintf("%.4f", d.Scores.Fuzzy),
					fmt.Sprintf("%.4f", d.WeightedTotal),
				})
			}
		case []map[string]any:
			if len(v) == 0 {
				return
			}
			// Use sorted keys as headers for stability.
			keys := make([]string, 0, len(v[0]))
			for k := range v[0] {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			// Escape headers for LaTeX while keeping original keys for lookup.
			table.headers = make([]string, len(keys))
			for i, k := range keys {
				table.headers[i] = LaTeXEscape(k)
			}

			for _, m := range v {
				row := make([]any, len(keys))
				for i, k := range keys {
					val := m[k]
					if s, ok := val.(string); ok {
						val = LaTeXEscape(s)
					}
					row[i] = val
				}
				table.rows = append(table.rows, row)
			}
		}
	}
}

func (table *Table) SetOutput(out io.Writer) {
	table.out = out
}

func TableWithOutput(out io.Writer) tableOpts {
	return func(table *Table) {
		table.out = out
	}
}
