package phasedial

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
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
	section tools.ExperimentSection,
) []tools.Artifact {
	n := len(tableData)
	slug := tools.Slugify(expName)
	artifacts := []tools.Artifact{}

	panels := trialmap.TwoScorePanels(tableData, score, trialmap.StandardTwoPanel(), nil)

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

	artifacts = append(artifacts, tools.Artifact{
		Type:     tools.ArtifactProse,
		FileName: slug + "_section.tex",
		Data: tools.ProseData{
			Template: projector.ExperimentSectionTmpl,
			Data:     section,
		},
	})

	return artifacts
}

