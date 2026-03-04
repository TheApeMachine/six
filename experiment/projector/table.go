package projector

import (
	"bytes"
	_ "embed"
	"io"
	"os"
	"sort"
	"text/template"
)

//go:embed table.tmpl
var tableTmpl string

type Table struct {
	data []map[string]any
	out  io.Writer
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

	var rawHeaders []string     // used for map key lookup
	var displayHeaders []string // LaTeX-safe, used in the rendered table header
	if len(table.data) > 0 {
		for k := range table.data[0] {
			rawHeaders = append(rawHeaders, k)
		}
		sort.Strings(rawHeaders)
		for _, k := range rawHeaders {
			displayHeaders = append(displayHeaders, LaTeXEscape(k))
		}
	}

	var rows [][]any
	for _, rowMap := range table.data {
		var row []any
		for _, h := range rawHeaders {
			v := rowMap[h]
			if s, ok := v.(string); ok {
				v = LaTeXEscape(s)
			}
			row = append(row, v)
		}
		rows = append(rows, row)
	}

	templateData := struct {
		Headers []string
		Rows    [][]any
	}{
		Headers: displayHeaders,
		Rows:    rows,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return err
	}

	_, err = table.out.Write(buf.Bytes())
	return err
}

func TableWithData(data []map[string]any) tableOpts {
	return func(table *Table) {
		table.data = data
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
