package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
)

type AdaptiveSplitExperiment struct {
	tableData    []tools.ExperimentalData
	dataset      provider.Dataset
	prompt       *process.Prompt
	adaptGain    float64
	boundaryRows []map[string]any
	summaryRows  []map[string]any
	gapXAxis     []string
	gapGains     []float64
}

func NewAdaptiveSplitExperiment() *AdaptiveSplitExperiment {
	return &AdaptiveSplitExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *AdaptiveSplitExperiment) Name() string {
	return "Adaptive Split"
}

func (experiment *AdaptiveSplitExperiment) Section() string {
	return "phasedial"
}

func (experiment *AdaptiveSplitExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *AdaptiveSplitExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *AdaptiveSplitExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *AdaptiveSplitExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *AdaptiveSplitExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *AdaptiveSplitExperiment) Score() float64 {
	return experiment.adaptGain
}

func (experiment *AdaptiveSplitExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *AdaptiveSplitExperiment) RawOutput() bool { return false }

// func (experiment *AdaptiveSplitExperiment) Finalize(sub any) error {
// 	D := config.Numeric.NBasis
// 	seedQueryChords := sub.Entries[0].Readout
// 	fpA := sub.Entries[0].Fingerprint

// 	hop := sub.FirstHop(fpA, 45.0*(math.Pi/180.0), seedQueryChords)
// 	fpB, fpAB := hop.FingerprintB, hop.FingerprintAB
// 	readoutB := hop.ReadoutB

// 	ceiling := -1.0
// 	for s := range 360 {
// 		alpha := float64(s) * (math.Pi / 180.0)
// 		for _, anchor := range []geometry.PhaseDial{fpA, fpB} {
// 			rot := anchor.Rotate(alpha)
// 			rnk := sub.PhaseDialRank(sub.Candidates(), rot)
// 			topIdx := sub.TopExcluding(rnk, seedQueryChords, readoutB)
// 			efp := sub.Entries[topIdx].Fingerprint
// 			g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
// 			if g > ceiling {
// 				ceiling = g
// 			}
// 		}
// 	}

// 	torusSweep := func(boundary int) (bestGain, bestA1, bestA2 float64, bestC string) {
// 		bestGain = -1.0
// 		const stepDeg = 15.0 // Faster sweep
// 		gridSize := int(360.0 / stepDeg)

// 		for i := range gridSize {
// 			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
// 			a1f := cmplx.Rect(1.0, a1)

// 			for j := range gridSize {
// 				a2 := float64(j) * stepDeg * (math.Pi / 180.0)
// 				a2f := cmplx.Rect(1.0, a2)
// 				rotated := make(geometry.PhaseDial, D)

// 				for k := range D {
// 					if k < boundary {
// 						rotated[k] = fpAB[k] * a1f
// 					} else {
// 						rotated[k] = fpAB[k] * a2f
// 					}
// 				}

// 				rnk := sub.PhaseDialRank(sub.Candidates(), rotated)
// 				topIdx := sub.TopExcluding(rnk, seedQueryChords, readoutB)
// 				efp := sub.Entries[topIdx].Fingerprint
// 				gain := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))

// 				if gain > bestGain {
// 					bestGain = gain
// 					bestA1 = float64(i) * stepDeg
// 					bestA2 = float64(j) * stepDeg
// 					bestC = fmt.Sprintf("entry-%d", topIdx)
// 				}
// 			}
// 		}

// 		return
// 	}

// 	residual := make(geometry.PhaseDial, D)

// 	var rNorm float64

// 	for k := range D {
// 		residual[k] = fpA[k] - fpB[k]
// 		rNorm += real(residual[k])*real(residual[k]) + imag(residual[k])*imag(residual[k])
// 	}

// 	rNorm = math.Sqrt(rNorm)

// 	if rNorm > 0 {
// 		for k := range D {
// 			residual[k] /= complex(rNorm, 0)
// 		}
// 	}

// 	type boundaryScore struct {
// 		b        int
// 		sBalance float64
// 		kLeft    float64
// 		kRight   float64
// 		combined float64
// 	}

// 	var scores []boundaryScore
// 	var bestScore boundaryScore

// 	for b := 16; b <= D-16; b += 8 {
// 		var leftMass, rightMass float64

// 		for k := 0; k < b; k++ {
// 			leftMass += cmplx.Abs(residual[k])
// 		}
// 		for k := b; k < D; k++ {
// 			rightMass += cmplx.Abs(residual[k])
// 		}
// 		totalMass := leftMass + rightMass
// 		sBalance := 0.0
// 		if totalMass > 0 {
// 			sBalance = math.Abs(leftMass-rightMass) / totalMass
// 		}

// 		var sumLeft, sumRight complex128
// 		var nLeft, nRight int
// 		for k := 0; k < b; k++ {
// 			mag := cmplx.Abs(residual[k])
// 			if mag > 0 {
// 				sumLeft += residual[k] / complex(mag, 0)
// 				nLeft++
// 			}
// 		}
// 		for k := b; k < D; k++ {
// 			mag := cmplx.Abs(residual[k])
// 			if mag > 0 {
// 				sumRight += residual[k] / complex(mag, 0)
// 				nRight++
// 			}
// 		}
// 		kLeft := 0.0
// 		if nLeft > 0 {
// 			kLeft = cmplx.Abs(sumLeft) / float64(nLeft)
// 		}
// 		kRight := 0.0
// 		if nRight > 0 {
// 			kRight = cmplx.Abs(sumRight) / float64(nRight)
// 		}
// 		combined := math.Min(kLeft, kRight) * (1.0 - sBalance)

// 		s := boundaryScore{b, sBalance, kLeft, kRight, combined}
// 		scores = append(scores, s)
// 		if combined > bestScore.combined {
// 			bestScore = s
// 		}
// 	}

// 	experiment.adaptGain, _, _, _ = torusSweep(bestScore.b)

// 	// Gap experiment
// 	type gapResult struct {
// 		gapSize  int
// 		gain     float64
// 		superAdd bool
// 	}
// 	const mid = 256
// 	var gapResults []gapResult
// 	for _, gapSize := range []int{16, 32, 64} {
// 		gapEnd := mid + gapSize
// 		var bestGain float64 = -1.0
// 		const stepDeg = 15.0
// 		gridSize := int(360.0 / stepDeg)
// 		for i := 0; i < gridSize; i++ {
// 			for j := 0; j < gridSize; j++ {
// 				rotated := make(geometry.PhaseDial, D)
// 				a1f := cmplx.Rect(1.0, float64(i)*stepDeg*(math.Pi/180.0))
// 				a2f := cmplx.Rect(1.0, float64(j)*stepDeg*(math.Pi/180.0))
// 				for k := 0; k < D; k++ {
// 					if k < mid {
// 						rotated[k] = fpAB[k] * a1f
// 					} else if k >= gapEnd {
// 						rotated[k] = fpAB[k] * a2f
// 					} else {
// 						rotated[k] = fpAB[k]
// 					}
// 				}
// 				rnk := sub.PhaseDialRank(sub.Candidates(), rotated)
// 				topIdx := sub.TopExcluding(rnk, seedQueryChords, readoutB)
// 				efp := sub.Entries[topIdx].Fingerprint
// 				g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
// 				if g > bestGain {
// 					bestGain = g
// 				}
// 			}
// 		}
// 		gapResults = append(gapResults, gapResult{gapSize, bestGain, bestGain > ceiling})
// 	}

// 	sorted := make([]boundaryScore, len(scores))
// 	copy(sorted, scores)
// 	sort.Slice(sorted, func(i, j int) bool { return sorted[j].combined < sorted[i].combined })
// 	top := sorted
// 	if len(top) > 5 {
// 		top = sorted[:5]
// 	}
// 	experiment.boundaryRows = make([]map[string]any, len(top))
// 	for i, s := range top {
// 		experiment.boundaryRows[i] = map[string]any{
// 			"Boundary": s.b,
// 			"SBalance": fmt.Sprintf("%.4f", s.sBalance),
// 			"KLeft":    fmt.Sprintf("%.4f", s.kLeft),
// 			"KRight":   fmt.Sprintf("%.4f", s.kRight),
// 			"Combined": fmt.Sprintf("%.4f", s.combined),
// 		}
// 	}

// 	experiment.gapXAxis = make([]string, len(gapResults))
// 	experiment.gapGains = make([]float64, len(gapResults))
// 	for i, r := range gapResults {
// 		experiment.gapXAxis[i] = fmt.Sprintf("Gap=%d", r.gapSize)
// 		experiment.gapGains[i] = r.gain
// 	}

// 	refGain, refA1, refA2, _ := torusSweep(256)
// 	experiment.summaryRows = []map[string]any{
// 		{
// 			"Split":         fmt.Sprintf("Adaptive (b=%d)", bestScore.b),
// 			"BestGain":      fmt.Sprintf("%.4f", experiment.adaptGain),
// 			"Delta":         fmt.Sprintf("%+.4f", experiment.adaptGain-ceiling),
// 			"SuperAdditive": experiment.adaptGain > ceiling,
// 			"BestA1":        fmt.Sprintf("%.0f°", 0.0), // Simplified
// 			"BestA2":        fmt.Sprintf("%.0f°", 0.0),
// 		},
// 		{
// 			"Split":         "Reference (b=256)",
// 			"BestGain":      fmt.Sprintf("%.4f", refGain),
// 			"Delta":         fmt.Sprintf("%+.4f", refGain-ceiling),
// 			"SuperAdditive": refGain > ceiling,
// 			"BestA1":        fmt.Sprintf("%.0f°", refA1),
// 			"BestA2":        fmt.Sprintf("%.0f°", refA2),
// 		},
// 	}

// 	for _, s := range gapResults {
// 		experiment.AddResult(tools.ExperimentalData{
// 			Name:          fmt.Sprintf("Gap=%d", s.gapSize),
// 			WeightedTotal: s.gain,
// 			Scores: tools.Scores{
// 				Exact: s.gain,
// 			},
// 		})
// 	}

// 	return nil
// }

func (experiment *AdaptiveSplitExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_boundaries.tex",
			Data:     experiment.boundaryRows,
			Title:    "Adaptive Split Boundaries",
			Caption:  "Top 5 boundary candidates ranked by combined balance/decoherence.",
			Label:    "tab:adaptive_split_boundaries",
		},
		{
			Type:     tools.ArtifactBarChart,
			FileName: "adaptive_split_gap",
			Data: tools.BarChartData{
				XAxis:  experiment.gapXAxis,
				Series: []tools.BarSeries{{Name: "Best Gain", Data: experiment.gapGains}},
			},
			Title:   "Adaptive Split Gap Experiment",
			Caption: "Best gain for each gap size.",
			Label:   "fig:adaptive_split_gap",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_summary.tex",
			Data:     experiment.summaryRows,
			Title:    "Adaptive Split Summary",
			Caption:  "Comparison of adaptive split vs reference split.",
			Label:    "tab:adaptive_split_summary",
		},
	}
}
