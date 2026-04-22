package projector

import (
	"io"
	"os"
)

/*
PolarPoint is one entity plotted on a polar chart.
Angle is in degrees (0–360), Radius is 0..1 (normalised similarity).
*/
type PolarPoint struct {
	Label  string  `json:"label"`
	Angle  float64 `json:"angle"`
	Radius float64 `json:"radius"`
	Color  string  `json:"color"`
}

/*
PolarSnapshot is one temporal snapshot panel in the 2×2 grid.
Channels are radial indicator lines (angles in degrees).
*/
type PolarSnapshot struct {
	Title    string       `json:"title"`
	Points   []PolarPoint `json:"points"`
	Channels []float64    `json:"channels"`
}

/*
PolarConstraintData is the self-contained artifact payload for a 2×2 polar
constraint figure. It lives in the projector package to avoid circular
imports with the experiment package.
*/
type PolarConstraintData struct {
	Snapshots []PolarSnapshot `json:"snapshots"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	Title     string          `json:"title"`
	Caption   string          `json:"caption"`
	Label     string          `json:"label"`
}

/*
PolarConstraintChart renders a 2×2 grid of polar scatter plots showing
how PhaseDial similarity narrows across four temporal snapshots.
*/
type PolarConstraintChart struct {
	out       io.Writer
	snapshots []PolarSnapshot
	title     string
	caption   string
	label     string
	filename  string
	outDir    string
	width     int
	height    int
}

type polarConstraintOpts func(*PolarConstraintChart)

func NewPolarConstraintChart(opts ...polarConstraintOpts) *PolarConstraintChart {
	pc := &PolarConstraintChart{
		out:      os.Stdout,
		filename: "polar_constraint",
		outDir:   ".",
		width:    1200,
		height:   900,
	}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}

func PolarConstraintWithSnapshots(snaps []PolarSnapshot) polarConstraintOpts {
	return func(pc *PolarConstraintChart) { pc.snapshots = snaps }
}

func PolarConstraintWithMeta(title, caption, label string) polarConstraintOpts {
	return func(pc *PolarConstraintChart) {
		pc.title = title
		pc.caption = caption
		pc.label = label
	}
}

func PolarConstraintWithOutput(outDir, filename string) polarConstraintOpts {
	return func(pc *PolarConstraintChart) { pc.outDir = outDir; pc.filename = filename }
}

func PolarConstraintWithSize(w, h int) polarConstraintOpts {
	return func(pc *PolarConstraintChart) { pc.width = w; pc.height = h }
}

func (pc *PolarConstraintChart) SetOutput(out io.Writer) { pc.out = out }

/*
Generate renders the polar grid PDF and writes a LaTeX figure stub.
*/
func (pc *PolarConstraintChart) Generate() error {
	spec := struct {
		Title     string          `json:"title"`
		Width     int             `json:"width"`
		Height    int             `json:"height"`
		Snapshots []PolarSnapshot `json:"snapshots"`
	}{pc.title, pc.width, pc.height, pc.snapshots}

	if err := runPython("polar", spec, pc.outDir, pc.filename); err != nil {
		return err
	}
	return emitFigure(pc.filename, pc.caption, pc.label, pc.out)
}

/*
WritePolarConstraint is the top-level write helper called by the reporter.
*/
func WritePolarConstraint(
	data PolarConstraintData,
	outDir, filename string,
	out io.Writer,
) error {
	pc := NewPolarConstraintChart(
		PolarConstraintWithSnapshots(data.Snapshots),
		PolarConstraintWithMeta(data.Title, data.Caption, data.Label),
		PolarConstraintWithOutput(outDir, filename),
		PolarConstraintWithSize(data.Width, data.Height),
	)

	if out != nil {
		pc.SetOutput(out)
	}

	return pc.Generate()
}
