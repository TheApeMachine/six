package projector

import (
	"encoding/json"
	"fmt"
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
constraint figure.  It lives in the projector package to avoid circular
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
Each panel plots suspects and evidence as dots at (radius=similarity,
angle=phase angle derived from PhaseDial encoding).
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
Generate renders the 4-panel polar chart to PDF and writes a LaTeX figure stub.
*/
func (pc *PolarConstraintChart) Generate() error {
	snapJSON, err := json.Marshal(pc.snapshots)
	if err != nil {
		return fmt.Errorf("polar constraint marshal: %w", err)
	}

	script := buildPolarScript(string(snapJSON))

	html, err := renderChartHTML(pc.title, pc.width, pc.height, script)
	if err != nil {
		return err
	}

	if err := renderAndExport(html, pc.outDir, pc.filename, pc.width, pc.height); err != nil {
		return err
	}

	return emitFigure(pc.filename, pc.caption, pc.label, pc.out)
}

func buildPolarScript(snapJSON string) string {
	return fmt.Sprintf(`
const snapshots = %s;

const chart = echarts.init(document.getElementById('chart'), null, { renderer: 'svg' });

const centres = [
    { cx: '25%%', cy: '27%%' },
    { cx: '75%%', cy: '27%%' },
    { cx: '25%%', cy: '77%%' },
    { cx: '75%%', cy: '77%%' }
];
const radius = '38%%';

const polarAxes = [];
const angleAxes = [];
const polars    = [];
const seriesArr = [];
const titlesArr = [];

const SUSPECT_COLORS = ['#3b82f6', '#ec4899', '#8b5cf6', '#10b981'];
const EVIDENCE_COLOR = '#1e293b';

snapshots.forEach((snap, si) => {
    const c = centres[si];
    polars.push({ center: [c.cx, c.cy], radius: radius });

    polarAxes.push({
        id: si, polarIndex: si, min: 0, max: 1, splitNumber: 4,
        axisLine: { lineStyle: { color: '#94a3b8' } },
        splitLine: { lineStyle: { color: '#e2e8f0', type: 'dashed' } },
        axisLabel: { color: '#64748b', fontSize: 9,
            formatter: v => v.toFixed(2) }
    });
    angleAxes.push({
        id: si, polarIndex: si, type: 'value',
        min: 0, max: 360, interval: 45,
        startAngle: 90,
        axisLine: { show: true, lineStyle: { color: '#94a3b8' } },
        axisTick: { show: true },
        axisLabel: { color: '#64748b', fontSize: 9,
            formatter: v => v + '°' }
    });

    if (snap.title) {
        titlesArr.push({
            text: '(' + String.fromCharCode(65 + si) + ')  ' + snap.title,
            left: si %% 2 === 0 ? '2%%' : '52%%',
            top:  si < 2 ? '2%%' : '52%%',
            textStyle: { color: '#0f172a', fontSize: 12, fontWeight: 'normal' }
        });
    }

    // Channel indicator lines — thin grey radials at fixed angles.
    (snap.channels || []).forEach((angleDeg, ci) => {
        seriesArr.push({
            type: 'line',
            coordinateSystem: 'polar',
            polarIndex: si,
            name: '_ch' + si + '_' + ci,
            data: [[0, angleDeg], [1, angleDeg]],
            lineStyle: { color: '#94a3b8', width: 1 },
            symbol: 'none', silent: true, legendHoverLink: false
        });
    });

    // One scatter dot per entity.
    snap.points.forEach((pt, pi) => {
        const isSpecial = pt.label === 'EVIDENCE' || pt.label === 'MID';
        const color = pt.color || (isSpecial ? EVIDENCE_COLOR
                                             : SUSPECT_COLORS[pi %% SUSPECT_COLORS.length]);
        seriesArr.push({
            type: 'scatter',
            coordinateSystem: 'polar',
            polarIndex: si,
            name: pt.label,
            data: [[pt.radius, pt.angle]],
            symbolSize: isSpecial ? 14 : 18,
            itemStyle: { color },
            label: {
                show: true,
                formatter: pt.label,
                position: 'right',
                color: color,
                fontSize: 11,
                fontWeight: 'bold'
            }
        });
    });
});

chart.setOption({
    backgroundColor: 'transparent',
    animation:  false,
    title:      titlesArr,
    polar:      polars,
    radiusAxis: polarAxes,
    angleAxis:  angleAxes,
    series:     seriesArr,
    legend:     { show: false }
});
`, snapJSON)
}

/*
WritePolarConstraint is the top-level write helper called by the reporter.
*/
func WritePolarConstraint(
	data PolarConstraintData,
	outDir, filename string,
	out io.Writer,
) error {
	defer TriggerAutoBuild()

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
