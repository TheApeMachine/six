package projector

import (
	_ "embed"
	"encoding/json"
	"io"
	"os"
)

//go:embed imagestrip_script.tmpl
var imageStripScriptTmpl string

const (
	imageStripWidth      = 800
	imageStripRowHeight  = 180
	imageStripBaseHeight = 100
)

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

func imageStripDimensions(rows int) (width, height int) {
	return imageStripWidth, rows*imageStripRowHeight + imageStripBaseHeight
}

func (is *ImageStrip) Generate() error {
	dataJSON, err := json.Marshal(is.rows)
	if err != nil {
		return err
	}
	script := execTemplate(imageStripScriptTmpl, struct {
		DataJSON string
	}{string(dataJSON)})

	width, height := imageStripDimensions(len(is.rows))
	html, err := renderChartHTML(is.title, width, height, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, is.outDir, is.filename, width, height); err != nil {
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
