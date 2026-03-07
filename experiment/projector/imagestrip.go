package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed imagestrip_script.tmpl
var imageStripScriptTmpl string

type ImageStripRow struct {
	Original      string `json:"original"`      // Base64 encoded image
	Masked        string `json:"masked"`        // Base64 encoded image
	Reconstructed string `json:"reconstructed"` // Base64 encoded image
	Label         string `json:"label"`
}

type ImageStrip struct {
	out      io.Writer
	title    string
	rows     []ImageStripRow
	caption  string
	label    string
	filename string
	outDir   string
}

type imageStripOpts func(*ImageStrip)

func NewImageStrip(opts ...imageStripOpts) *ImageStrip {
	is := &ImageStrip{out: os.Stdout, filename: "imagestrip", outDir: "."}
	for _, opt := range opts {
		opt(is)
	}
	return is
}

func (is *ImageStrip) SetOutput(out io.Writer) { is.out = out }

func (is *ImageStrip) Generate() error {
	dataJSON, _ := json.Marshal(is.rows)
	script := execTemplate(imageStripScriptTmpl, struct {
		DataJSON string
	}{string(dataJSON)})

	height := len(is.rows)*180 + 100 // Estimate dynamic height
	html, err := renderChartHTML(is.title, 800, height, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, is.outDir, is.filename, 800, height); err != nil {
		return err
	}
	return emitFigure(is.filename, is.caption, is.label, is.out)
}

func ImageStripWithData(rows []ImageStripRow) imageStripOpts {
	return func(is *ImageStrip) {
		is.rows = rows
	}
}

func ImageStripWithMeta(title, caption, label string) imageStripOpts {
	return func(is *ImageStrip) { is.title = title; is.caption = caption; is.label = label }
}

func ImageStripWithOutput(outDir, filename string) imageStripOpts {
	return func(is *ImageStrip) { is.outDir = outDir; is.filename = filename }
}
