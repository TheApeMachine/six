package trialmap

import (
	"fmt"
	"strconv"

	tools "github.com/theapemachine/six/experiment"
)

/*
TwoPanelGrids fixes ECharts grid slots for the standard score-fingerprint
heatmap plus weighted-score chart pair. Different experiments nudge top
margins or horizontal splits while keeping the same data plumbing.
*/
type TwoPanelGrids struct {
	HeatLeft      string
	HeatRight     string
	HeatTop       string
	HeatBottom    string
	VMRight       string
	ChartLeft     string
	ChartRight    string
	ChartTop      string
	ChartBottom   string
	HeatTitle     string
	ChartTitle    string
	BarName       string
	HeatXAxisName string
}

/*
StandardTwoPanel is the textgen / phasedial / semantic-algebra layout.
*/
func StandardTwoPanel() TwoPanelGrids {
	return TwoPanelGrids{
		HeatLeft:    "5%",
		HeatRight:   "57%",
		HeatTop:     "14%",
		HeatBottom:  "18%",
		VMRight:     "43%",
		ChartLeft:   "58%",
		ChartRight:  "4%",
		ChartTop:    "14%",
		ChartBottom: "18%",
		HeatTitle:   "Score Fingerprint",
		ChartTitle:  "Weighted Score",
		BarName:     "Score",
	}
}

/*
ScalingSequencerTwoPanel matches scaling/sequencer margins and copy.
*/
func ScalingSequencerTwoPanel() TwoPanelGrids {
	g := StandardTwoPanel()

	g.HeatTop = "12%"
	g.ChartTop = "12%"
	g.ChartTitle = "Weighted Score per Sample"
	g.BarName = "Weighted"

	return g
}

/*
StandardDenseTop uses 12\% top margins on both panels (cross-domain heat,
scaling-style density) while keeping standard titles and bar naming.
*/
func StandardDenseTop() TwoPanelGrids {
	g := StandardTwoPanel()

	g.HeatTop = "12%"
	g.ChartTop = "12%"

	return g
}

/*
ProteinFingerprintBarOnly is panels A+B in the protein artifact; the
alignment strip is appended separately.
*/
func ProteinFingerprintBarOnly() TwoPanelGrids {
	return TwoPanelGrids{
		HeatLeft:    "4%",
		HeatRight:   "72%",
		HeatTop:     "12%",
		HeatBottom:  "20%",
		VMRight:     "27%",
		ChartLeft:   "30%",
		ChartRight:  "52%",
		ChartTop:    "12%",
		ChartBottom: "20%",
		HeatTitle:   "A: Score Fingerprint",
		ChartTitle:  "B: Weighted Score",
		BarName:     "Weighted",
	}
}

var scoreColumns = []string{"Exact", "Partial", "Fuzzy", "Weighted"}

/*
TwoScorePanels builds the heatmap + bar+dashed-mean pair from per-sample
experimental rows. sampleLabels must have length len(rows) when non-nil;
otherwise labels are S1…Sn or Q1…Qn based on grids (Babi uses Q prefix
when sampleLabels is nil by checking HeatXAxisName).
*/
func TwoScorePanels(
	rows []tools.ExperimentalData,
	meanScore float64,
	grids TwoPanelGrids,
	sampleLabels []string,
) []tools.Panel {
	n := len(rows)

	if n == 0 {
		return nil
	}

	labels := sampleLabels

	if len(labels) != n {
		prefix := "S"

		if grids.HeatXAxisName == "Score Dimension" {
			prefix = "Q"
		}

		labels = make([]string, n)

		for idx := range labels {
			labels[idx] = prefix + strconv.Itoa(idx+1)
		}
	}

	cells := n * 4
	heatFlat := make([]any, cells*3)
	heatData := make([][]any, cells)

	for index := range heatData {
		base := index * 3
		heatData[index] = heatFlat[base : base+3]
	}

	idx := 0
	weightedVals := make([]float64, n)
	meanLine := make([]float64, n)

	for sIdx, row := range rows {
		for cIdx, v := range []float64{
			row.Scores.Exact,
			row.Scores.Partial,
			row.Scores.Fuzzy,
			row.WeightedTotal,
		} {
			base := idx * 3
			heatFlat[base] = cIdx
			heatFlat[base+1] = sIdx
			heatFlat[base+2] = v
			idx++
		}

		weightedVals[sIdx] = row.WeightedTotal
		meanLine[sIdx] = meanScore
	}

	return []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       grids.HeatTitle,
			GridLeft:    grids.HeatLeft,
			GridRight:   grids.HeatRight,
			GridTop:     grids.HeatTop,
			GridBottom:  grids.HeatBottom,
			XLabels:     scoreColumns,
			XAxisName:   grids.HeatXAxisName,
			XShow:       true,
			YLabels:     labels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     grids.VMRight,
		},
		{
			Kind:       "chart",
			Title:      grids.ChartTitle,
			GridLeft:   grids.ChartLeft,
			GridRight:  grids.ChartRight,
			GridTop:    grids.ChartTop,
			GridBottom: grids.ChartBottom,
			XLabels:    labels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: grids.BarName, Kind: "bar", BarWidth: "55%", Data: weightedVals},
				{Name: fmt.Sprintf("Mean (%.2f)", meanScore), Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}
}
