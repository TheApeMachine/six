package phasedial

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
)

/*
PhasedialSectionArtifacts builds the standard prose+figure artifact pair
for any phasedial experiment. Every phasedial experiment must produce
at minimum a trial-outcome-map figure and a LaTeX prose section for
inclusion in the paper.
*/
func PhasedialSectionArtifacts(
	expName string,
	tableData []tools.ExperimentalData,
	score float64,
	proseTemplate string,
	proseData map[string]any,
) []tools.Artifact {
	n := len(tableData)
	slug := tools.Slugify(expName)
	artifacts := []tools.Artifact{}

	panels := phasedialTrialMapPanels(tableData, score)

	if len(panels) > 0 {
		artifacts = append(artifacts, tools.Artifact{
			Type:     tools.ArtifactMultiPanel,
			FileName: slug + "_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1100,
				Height: 420,
			},
			Title:   expName + " — Trial Outcome Map",
			Caption: fmt.Sprintf("Score fingerprint and per-sample weighted score. N=%d.", n),
			Label:   "fig:" + slug + "_map",
		})
	}

	if proseTemplate != "" {
		artifacts = append(artifacts, tools.Artifact{
			Type:     tools.ArtifactProse,
			FileName: slug + "_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data:     proseData,
			},
		})
	}

	return artifacts
}

/*
phasedialTrialMapPanels builds the standard two-panel trial outcome map:
left panel = score fingerprint heatmap, right panel = weighted score bar.
*/
func phasedialTrialMapPanels(tableData []tools.ExperimentalData, score float64) []tools.Panel {
	n := len(tableData)
	if n == 0 {
		return nil
	}

	sampleLabels := make([]string, n)
	for i := range sampleLabels {
		sampleLabels[i] = fmt.Sprintf("S%d", i+1)
	}

	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}
	heatData := make([][]any, 0, n*4)
	weightedVals := make([]float64, n)
	meanLine := make([]float64, n)

	for sIdx, row := range tableData {
		for cIdx, v := range []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal} {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}

		weightedVals[sIdx] = row.WeightedTotal
		meanLine[sIdx] = score
	}

	return []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       "Score Fingerprint",
			GridLeft:    "5%",
			GridRight:   "57%",
			GridTop:     "14%",
			GridBottom:  "18%",
			XLabels:     scoreLabels,
			XShow:       true,
			YLabels:     sampleLabels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     "43%",
		},
		{
			Kind:       "chart",
			Title:      "Weighted Score",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "14%",
			GridBottom: "18%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Score", Kind: "bar", BarWidth: "55%", Data: weightedVals},
				{Name: fmt.Sprintf("Mean (%.2f)", score), Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}
}
