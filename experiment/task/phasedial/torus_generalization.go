package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"
	"math/rand"
	"sort"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
TorusGeneralizationExperiment tests how different split strategies
(contiguous, random, energy-based) affect the super-additivity of
the torus navigation. It demonstrates that meaningful geometric splits
are necessary for coherent navigation.
*/
type TorusGeneralizationExperiment struct {
	tableData   []tools.ExperimentalData
	dataset     provider.Dataset
	prompt      *tokenizer.Prompt
	comboSeries []projector.ComboSeries
	tableRows   []map[string]any
	xAxis       []string
}

func NewTorusGeneralizationExperiment() *TorusGeneralizationExperiment {
	return &TorusGeneralizationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *TorusGeneralizationExperiment) Name() string {
	return "Torus Generalization"
}

func (experiment *TorusGeneralizationExperiment) Section() string {
	return "phasedial"
}

func (experiment *TorusGeneralizationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TorusGeneralizationExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *TorusGeneralizationExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *TorusGeneralizationExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TorusGeneralizationExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *TorusGeneralizationExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *TorusGeneralizationExperiment) TableData() any {
	return experiment.tableData
}

// Helpers ported from the original test for local use
func localContiguousSplit(numAxes int, boundaries []int) []int {
	dimMap := make([]int, config.Numeric.NBasis)
	sub := 0
	for k := 0; k < config.Numeric.NBasis; k++ {
		if sub < numAxes-1 && k >= boundaries[sub] {
			sub++
		}
		dimMap[k] = sub
	}
	return dimMap
}

func localRandomSplit(numAxes, dimsPerAxis int, seed int64) []int {
	rng := rand.New(rand.NewSource(seed))
	perm := rng.Perm(config.Numeric.NBasis)
	dimMap := make([]int, config.Numeric.NBasis)
	for i, dim := range perm {
		sub := i / dimsPerAxis
		if sub >= numAxes {
			sub = numAxes - 1
		}
		dimMap[dim] = sub
	}
	return dimMap
}

func localEnergySplit(fpA, fpB geometry.PhaseDial) []int {
	type dimE struct {
		k    int
		diff float64
	}
	dims := make([]dimE, config.Numeric.NBasis)
	for k := 0; k < config.Numeric.NBasis; k++ {
		eA := real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
		eB := real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
		dims[k] = dimE{k: k, diff: eA - eB}
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i].diff < dims[j].diff })
	dimMap := make([]int, config.Numeric.NBasis)
	half := config.Numeric.NBasis / 2
	for i, d := range dims {
		if i < half {
			dimMap[d.k] = 0
		} else {
			dimMap[d.k] = 1
		}
	}
	return dimMap
}

func (experiment *TorusGeneralizationExperiment) Finalize(sub *geometry.HybridSubstrate) error {
	seedQueries := []string{
		"Democracy requires individual sacrifice.",
		"Knowledge is power.",
		"Nature does not hurry, yet everything is accomplished.",
	}

	compute1DCeiling := func(fpA, fpB geometry.PhaseDial, excludeA, excludeB string) float64 {
		ceiling := -1.0
		for s := 0; s < 360; s++ {
			alpha := float64(s) * (math.Pi / 180.0)
			for _, anchor := range []geometry.PhaseDial{fpA, fpB} {
				rot := anchor.Rotate(alpha)
				rnk := sub.PhaseDialRank(sub.Candidates(), rot)
				topIdx := sub.TopExcluding(rnk, excludeA, excludeB)
				efp := sub.Entries[topIdx].Fingerprint
				g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
				if g > ceiling {
					ceiling = g
				}
			}
		}
		return ceiling
	}

	generalRotate := func(fp geometry.PhaseDial, numAxes int, dimMap []int, angles []float64) geometry.PhaseDial {
		factors := make([]complex128, numAxes)
		for i, a := range angles {
			factors[i] = cmplx.Rect(1.0, a)
		}
		rotated := make(geometry.PhaseDial, config.Numeric.NBasis)
		for k := 0; k < config.Numeric.NBasis; k++ {
			rotated[k] = fp[k] * factors[dimMap[k]]
		}
		return rotated
	}

	type splitResult struct {
		SplitName     string
		BestGain      float64
		SingleCeiling float64
		Delta         float64
		SuperAdditive bool
	}
	type seedResult struct {
		SeedQuery string
		TextB     string
		Splits    []splitResult
	}

	var allSeeds []seedResult
	for _, seedQuery := range seedQueries {
		fingerprintA := geometry.NewPhaseDial().Encode(seedQuery)
		hop := sub.FirstHop(fingerprintA, 45.0*(math.Pi/180.0), seedQuery)
		fpB, fpAB := hop.FingerprintB, hop.FingerprintAB
		textB := hop.TextB

		ceiling := compute1DCeiling(fingerprintA, fpB, seedQuery, textB)

		configs := []struct {
			name   string
			dimMap []int
		}{
			{"T²-256/256", localContiguousSplit(2, []int{256, 512})},
			{"T²-random", localRandomSplit(2, 256, 42)},
			{"T²-energy", localEnergySplit(fingerprintA, fpB)},
		}

		sr := seedResult{SeedQuery: seedQuery, TextB: textB}
		for _, cfg := range configs {
			const stepDeg = 10.0 // Faster sweep for experiment
			stepRad := stepDeg * (math.Pi / 180.0)
			gridSize := int(360.0 / stepDeg)
			var bestGain float64 = -1.0

			for i := 0; i < gridSize; i++ {
				for j := 0; j < gridSize; j++ {
					angles := []float64{float64(i) * stepRad, float64(j) * stepRad}
					rotatedAB := generalRotate(fpAB, 2, cfg.dimMap, angles)
					rnk := sub.PhaseDialRank(sub.Candidates(), rotatedAB)
					topIdx := sub.TopExcluding(rnk, seedQuery, textB)
					fpC := sub.Entries[topIdx].Fingerprint
					gain := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fpB))
					if gain > bestGain {
						bestGain = gain
					}
				}
			}
			sr.Splits = append(sr.Splits, splitResult{
				SplitName:     cfg.name,
				BestGain:      bestGain,
				SingleCeiling: ceiling,
				Delta:         bestGain - ceiling,
				SuperAdditive: bestGain > ceiling,
			})
		}
		allSeeds = append(allSeeds, sr)
	}

	experiment.xAxis = make([]string, len(allSeeds[0].Splits))
	for i, sp := range allSeeds[0].Splits {
		experiment.xAxis[i] = sp.SplitName
	}
	ceilingData := make([]float64, len(allSeeds[0].Splits))
	for i := range ceilingData {
		maxCeiling := 0.0
		for _, s := range allSeeds {
			if s.Splits[i].SingleCeiling > maxCeiling {
				maxCeiling = s.Splits[i].SingleCeiling
			}
		}
		ceilingData[i] = maxCeiling
	}

	for _, s := range allSeeds {
		gainData := make([]float64, len(s.Splits))
		for i, sp := range s.Splits {
			gainData[i] = sp.BestGain
		}
		experiment.comboSeries = append(experiment.comboSeries, projector.ComboSeries{
			Name: s.SeedQuery, Type: "bar", BarWidth: "12%", Data: gainData,
		})
	}
	experiment.comboSeries = append(experiment.comboSeries, projector.ComboSeries{
		Name: "1D Ceiling", Type: "dashed", Symbol: "circle", Data: ceilingData,
	})

	for _, s := range allSeeds {
		for _, sp := range s.Splits {
			experiment.tableRows = append(experiment.tableRows, map[string]any{
				"Seed":          s.SeedQuery,
				"Split":         sp.SplitName,
				"BestGain":      fmt.Sprintf("%.4f", sp.BestGain),
				"Delta":         fmt.Sprintf("%+.4f", sp.Delta),
				"SuperAdditive": sp.SuperAdditive,
			})

			experiment.AddResult(tools.ExperimentalData{
				Name:          sp.SplitName,
				WeightedTotal: sp.BestGain,
				Scores: tools.Scores{
					Exact:   sp.BestGain,
					Partial: sp.SingleCeiling,
					Fuzzy:   sp.Delta,
				},
			})
		}
	}

	return nil
}

func (experiment *TorusGeneralizationExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactComboChart,
			FileName: "torus_generalization",
			Data: map[string]any{
				"xAxis":  experiment.xAxis,
				"series": experiment.comboSeries,
				"xName":  "Split Configuration",
				"yName":  "Best Gain",
				"yMin":   0.0,
				"yMax":   0.25,
			},
			Title:   "Torus Generalization",
			Caption: "Best torus gain for each split configuration.",
			Label:   "fig:torus_generalization",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "torus_generalization_summary.tex",
			Data:     experiment.tableRows,
			Title:    "Torus Generalization Summary",
			Caption:  "Summary of best gains for each split and seed.",
			Label:    "tab:torus_generalization",
		},
	}
}
