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

	var headers []string
	if len(table.data) > 0 {
		for k := range table.data[0] {
			headers = append(headers, k)
		}
		sort.Strings(headers)
	}

	var rows [][]any
	for _, rowMap := range table.data {
		var row []any
		for _, h := range headers {
			row = append(row, rowMap[h])
		}
		rows = append(rows, row)
	}

	templateData := struct {
		Headers []string
		Rows    [][]any
	}{
		Headers: headers,
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
