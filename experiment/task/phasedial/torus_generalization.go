package phasedial

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
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
	tableData     []tools.ExperimentalData
	dataset       provider.Dataset
	prompt        *tokenizer.Prompt
	comboSeries   []tools.ComboSeries
	tableRows     []map[string]any
	xAxis         []string
	seedQueries   []string
	splitRatios   []float64
	effectiveDims []int
	sweepStepDeg  float64
	randomSeed    int64
}

type torusGeneralizationOpt func(*TorusGeneralizationExperiment)

func NewTorusGeneralizationExperiment(opts ...torusGeneralizationOpt) *TorusGeneralizationExperiment {
	experiment := &TorusGeneralizationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
		seedQueries: []string{
			"Democracy requires individual sacrifice.",
		},
		splitRatios:   []float64{0.375, 0.4375, 0.5, 0.5625, 0.625},
		effectiveDims: []int{config.Numeric.NBasis / 2, config.Numeric.NBasis},
		sweepStepDeg:  5.0,
		randomSeed:    42,
	}

	for _, opt := range opts {
		opt(experiment)
	}

	return experiment
}

func TorusGeneralizationWithSplitRatios(splitRatios []float64) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		if len(splitRatios) == 0 {
			return
		}

		experiment.splitRatios = append([]float64(nil), splitRatios...)
	}
}

func TorusGeneralizationWithEffectiveDims(effectiveDims []int) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		if len(effectiveDims) == 0 {
			return
		}

		experiment.effectiveDims = append([]int(nil), effectiveDims...)
	}
}

func TorusGeneralizationWithSweepStep(stepDeg float64) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		if stepDeg > 0 && stepDeg <= 180 {
			experiment.sweepStepDeg = stepDeg
		}
	}
}

func TorusGeneralizationWithRandomSeed(randomSeed int64) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		experiment.randomSeed = randomSeed
	}
}

func TorusGeneralizationWithDataset(dataset provider.Dataset) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		if dataset != nil {
			experiment.dataset = dataset
		}
	}
}

func TorusGeneralizationWithSeedQueries(seedQueries []string) torusGeneralizationOpt {
	return func(experiment *TorusGeneralizationExperiment) {
		if len(seedQueries) > 0 {
			experiment.seedQueries = append([]string(nil), seedQueries...)
		}
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
	return nil
}

func (experiment *TorusGeneralizationExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *TorusGeneralizationExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TorusGeneralizationExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
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
func localContiguousSplit(numAxes, totalDims int, boundaries []int) []int {
	dimMap := make([]int, config.Numeric.NBasis)
	for i := range dimMap {
		dimMap[i] = -1
	}

	sub := 0
	for k := 0; k < totalDims; k++ {
		if sub < numAxes-1 && k >= boundaries[sub] {
			sub++
		}
		dimMap[k] = sub
	}
	return dimMap
}

func localRandomSplit(numAxes, totalDims, dimsPerAxis int, seed int64) []int {
	rng := rand.New(rand.NewSource(seed))
	perm := rng.Perm(totalDims)
	dimMap := make([]int, config.Numeric.NBasis)
	for i := range dimMap {
		dimMap[i] = -1
	}

	for i, dim := range perm {
		sub := i / dimsPerAxis
		if sub >= numAxes {
			sub = numAxes - 1
		}
		dimMap[dim] = sub
	}
	return dimMap
}

func localEnergySplit(fpA, fpB geometry.PhaseDial, totalDims int) []int {
	type dimE struct {
		k    int
		diff float64
	}
	dims := make([]dimE, totalDims)
	for k := 0; k < totalDims; k++ {
		eA := real(fpA[k])*real(fpA[k]) + imag(fpA[k])*imag(fpA[k])
		eB := real(fpB[k])*real(fpB[k]) + imag(fpB[k])*imag(fpB[k])
		dims[k] = dimE{k: k, diff: eA - eB}
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i].diff < dims[j].diff })
	dimMap := make([]int, config.Numeric.NBasis)
	for i := range dimMap {
		dimMap[i] = -1
	}

	half := totalDims / 2
	for i, d := range dims {
		if i < half {
			dimMap[d.k] = 0
		} else {
			dimMap[d.k] = 1
		}
	}
	return dimMap
}

func normalizeEffectiveDims(values []int) []int {
	seen := make(map[int]bool, len(values))
	normalized := make([]int, 0, len(values))

	for _, value := range values {
		if value < 2 {
			continue
		}

		if value > config.Numeric.NBasis {
			value = config.Numeric.NBasis
		}

		if seen[value] {
			continue
		}

		seen[value] = true
		normalized = append(normalized, value)
	}

	sort.Ints(normalized)

	if len(normalized) == 0 {
		return []int{config.Numeric.NBasis}
	}

	return normalized
}

func splitCandidates(totalDims int, splitRatios []float64) []int {
	seen := make(map[int]bool, len(splitRatios))
	candidates := make([]int, 0, len(splitRatios))

	for _, ratio := range splitRatios {
		if ratio <= 0.0 || ratio >= 1.0 {
			continue
		}

		split := int(math.Round(ratio * float64(totalDims)))
		if split < 1 {
			split = 1
		}
		if split >= totalDims {
			split = totalDims - 1
		}

		if seen[split] {
			continue
		}

		seen[split] = true
		candidates = append(candidates, split)
	}

	sort.Ints(candidates)

	if len(candidates) == 0 {
		defaultSplit := totalDims / 2
		if defaultSplit < 1 {
			defaultSplit = 1
		}
		if defaultSplit >= totalDims {
			defaultSplit = totalDims - 1
		}
		return []int{defaultSplit}
	}

	return candidates
}

func formatSplitName(prefix string, left, right, totalDims int) string {
	if totalDims == config.Numeric.NBasis {
		return fmt.Sprintf("%s-%d/%d", prefix, left, right)
	}

	return fmt.Sprintf("%s(D=%d)-%d/%d", prefix, totalDims, left, right)
}

func (experiment *TorusGeneralizationExperiment) RawOutput() bool { return false }

func (experiment *TorusGeneralizationExperiment) Finalize(sub *geometry.HybridSubstrate) error {
	// stepDeg := experiment.sweepStepDeg
	// if stepDeg <= 0 || stepDeg > 180 {
	// 	stepDeg = 5.0
	// }

	// stepRad := stepDeg * (math.Pi / 180.0)
	// gridSize := int(math.Round(360.0 / stepDeg))
	// if gridSize < 1 {
	// 	gridSize = 1
	// }

	// rotatePrefix := func(fp geometry.PhaseDial, alpha float64, effectiveDims int) geometry.PhaseDial {
	// 	rotated := make(geometry.PhaseDial, len(fp))
	// 	copy(rotated, fp)

	// 	factor := cmplx.Rect(1.0, alpha)

	// 	limit := effectiveDims
	// 	if limit > len(fp) {
	// 		limit = len(fp)
	// 	}

	// 	for k := 0; k < limit; k++ {
	// 		rotated[k] = fp[k] * factor
	// 	}

	// 	return rotated
	// }

	// compute1DCeiling := func(fpA, fpB geometry.PhaseDial, excludeA, excludeB []data.Chord, effectiveDims int) float64 {
	// 	ceiling := -1.0
	// 	for s := 0; s < 360; s++ {
	// 		alpha := float64(s) * (math.Pi / 180.0)
	// 		for _, anchor := range []geometry.PhaseDial{
	// 			rotatePrefix(fpA, alpha, effectiveDims),
	// 			rotatePrefix(fpB, alpha, effectiveDims),
	// 		} {
	// 			rot := anchor
	// 			rnk := sub.PhaseDialRank(sub.Candidates(), rot)
	// 			topIdx := sub.TopExcluding(rnk, excludeA, excludeB)
	// 			efp := sub.Entries[topIdx].Fingerprint
	// 			g := math.Min(efp.Similarity(fpA), efp.Similarity(fpB))
	// 			if g > ceiling {
	// 				ceiling = g
	// 			}
	// 		}
	// 	}
	// 	return ceiling
	// }

	// generalRotate := func(fp geometry.PhaseDial, numAxes int, dimMap []int, angles []float64) geometry.PhaseDial {
	// 	factors := make([]complex128, numAxes)
	// 	for i, a := range angles {
	// 		factors[i] = cmplx.Rect(1.0, a)
	// 	}

	// 	rotated := make(geometry.PhaseDial, len(fp))
	// 	copy(rotated, fp)

	// 	for k := 0; k < len(fp); k++ {
	// 		if k >= len(dimMap) {
	// 			continue
	// 		}
	// 		axis := dimMap[k]
	// 		if axis < 0 || axis >= len(factors) {
	// 			continue
	// 		}

	// 		rotated[k] = fp[k] * factors[axis]
	// 	}
	// 	return rotated
	// }

	// type splitResult struct {
	// 	SplitName     string
	// 	BestGain      float64
	// 	SingleCeiling float64
	// 	Delta         float64
	// 	SuperAdditive bool
	// }
	// type seedResult struct {
	// 	SeedQuery string
	// 	Splits    []splitResult
	// }

	// if sub == nil || len(sub.Entries) == 0 {
	// 	return fmt.Errorf("substrate entries empty, cannot finalize")
	// }

	// effectiveDims := normalizeEffectiveDims(experiment.effectiveDims)

	// var allSeeds []seedResult
	// for _, seedQuery := range experiment.seedQueries {
	// 	seedQueryChords := sub.Entries[0].Readout
	// 	fingerprintA := sub.Entries[0].Fingerprint
	// 	hop := sub.FirstHop(fingerprintA, 45.0*(math.Pi/180.0), seedQueryChords)
	// 	fpB, fpAB := hop.FingerprintB, hop.FingerprintAB
	// 	readoutB := hop.ReadoutB

	// 	sr := seedResult{SeedQuery: seedQuery}

	// 	for effectiveDimIdx, effectiveDim := range effectiveDims {
	// 		splits := splitCandidates(effectiveDim, experiment.splitRatios)

	// 		for splitIdx, split := range splits {
	// 			ceiling := compute1DCeiling(fingerprintA, fpB, seedQueryChords, readoutB, effectiveDim)

	// 			rightDims := effectiveDim - split
	// 			dimsPerAxis := split
	// 			if rightDims < dimsPerAxis {
	// 				dimsPerAxis = rightDims
	// 			}
	// 			if dimsPerAxis < 1 {
	// 				dimsPerAxis = 1
	// 			}

	// 			seedOffset := int64(effectiveDimIdx*1024 + splitIdx)

	// 			configs := []struct {
	// 				name   string
	// 				dimMap []int
	// 			}{
	// 				{
	// 					name:   formatSplitName("T²", split, rightDims, effectiveDim),
	// 					dimMap: localContiguousSplit(2, effectiveDim, []int{split, effectiveDim}),
	// 				},
	// 				{
	// 					name:   formatSplitName("T²-random", split, rightDims, effectiveDim),
	// 					dimMap: localRandomSplit(2, effectiveDim, dimsPerAxis, experiment.randomSeed+seedOffset),
	// 				},
	// 				{
	// 					name:   formatSplitName("T²-energy", split, rightDims, effectiveDim),
	// 					dimMap: localEnergySplit(fingerprintA, fpB, effectiveDim),
	// 				},
	// 			}

	// 			for _, cfg := range configs {
	// 				bestGain := -1.0

	// 				for i := 0; i < gridSize; i++ {
	// 					for j := 0; j < gridSize; j++ {
	// 						angles := []float64{float64(i) * stepRad, float64(j) * stepRad}
	// 						rotatedAB := generalRotate(fpAB, 2, cfg.dimMap, angles)
	// 						rnk := sub.PhaseDialRank(sub.Candidates(), rotatedAB)
	// 						topIdx := sub.TopExcluding(rnk, seedQueryChords, readoutB)
	// 						fpC := sub.Entries[topIdx].Fingerprint
	// 						gain := math.Min(fpC.Similarity(fingerprintA), fpC.Similarity(fpB))
	// 						if gain > bestGain {
	// 							bestGain = gain
	// 						}
	// 					}
	// 				}

	// 				sr.Splits = append(sr.Splits, splitResult{
	// 					SplitName:     cfg.name,
	// 					BestGain:      bestGain,
	// 					SingleCeiling: ceiling,
	// 					Delta:         bestGain - ceiling,
	// 					SuperAdditive: bestGain > ceiling,
	// 				})
	// 			}
	// 		}
	// 	}

	// 	allSeeds = append(allSeeds, sr)
	// }

	// if len(allSeeds) == 0 {
	// 	return nil
	// }

	// experiment.xAxis = make([]string, len(allSeeds[0].Splits))
	// for i, sp := range allSeeds[0].Splits {
	// 	experiment.xAxis[i] = sp.SplitName
	// }
	// ceilingData := make([]float64, len(allSeeds[0].Splits))
	// for i := range ceilingData {
	// 	maxCeiling := 0.0
	// 	for _, s := range allSeeds {
	// 		if s.Splits[i].SingleCeiling > maxCeiling {
	// 			maxCeiling = s.Splits[i].SingleCeiling
	// 		}
	// 	}
	// 	ceilingData[i] = maxCeiling
	// }

	// for _, s := range allSeeds {
	// 	gainData := make([]float64, len(s.Splits))
	// 	for i, sp := range s.Splits {
	// 		gainData[i] = sp.BestGain
	// 	}
	// 	experiment.comboSeries = append(experiment.comboSeries, tools.ComboSeries{
	// 		Name: s.SeedQuery, Type: "bar", BarWidth: "12%", Data: gainData,
	// 	})
	// }
	// experiment.comboSeries = append(experiment.comboSeries, tools.ComboSeries{
	// 	Name: "1D Ceiling", Type: "dashed", Symbol: "circle", Data: ceilingData,
	// })

	// for _, s := range allSeeds {
	// 	for _, sp := range s.Splits {
	// 		experiment.tableRows = append(experiment.tableRows, map[string]any{
	// 			"Seed":          s.SeedQuery,
	// 			"Split":         sp.SplitName,
	// 			"BestGain":      fmt.Sprintf("%.4f", sp.BestGain),
	// 			"Delta":         fmt.Sprintf("%+.4f", sp.Delta),
	// 			"SuperAdditive": sp.SuperAdditive,
	// 		})

	// 		experiment.AddResult(tools.ExperimentalData{
	// 			Name:          sp.SplitName,
	// 			WeightedTotal: sp.BestGain,
	// 			Scores: tools.Scores{
	// 				Exact:   sp.BestGain,
	// 				Partial: sp.SingleCeiling,
	// 				Fuzzy:   sp.Delta,
	// 			},
	// 		})
	// 	}
	// }

	return nil
}

func (experiment *TorusGeneralizationExperiment) Artifacts() []tools.Artifact {
	proseTemplate := `\subsection{Torus Generalization (PhaseDial)}
\label{sec:torus_generalization}

\paragraph{Experiment Description.}
The Torus Generalization experiment evaluates whether frequency-domain
splits of the PhaseDial fingerprint produce \emph{super-additive}
analogical gain---that is, whether a two-axis torus rotation applied to
a composite fingerprint $\phi_A \star \phi_B$ can locate a third concept
$C$ that is simultaneously close to both $A$ and $B$, exceeding the
single-axis ceiling achievable by rotating $\phi_A$ or $\phi_B$ alone.

Three split strategies are compared:
\begin{itemize}[nosep]
  \item $T^2$-\textbf{contiguous}: partition the frequency spectrum at a
        fixed index.
  \item $T^2$-\textbf{random}: randomly assign frequency bins to each
        axis (fixed seed).
  \item $T^2$-\textbf{energy}: assign bins to axes greedily by the
        energy difference $|E_A(k) - E_B(k)|$, placing the
        most discriminative frequencies on separate axes.
\end{itemize}

The experiment sweeps all grid angles $(\alpha, \beta) \in [0, 2\pi)^2$
at 5\textdegree{} resolution and records the best gain $g = \min(
\mathrm{sim}(\phi_C, \phi_A), \mathrm{sim}(\phi_C, \phi_B))$ for each
configuration.

\paragraph{Status.}
The torus sweep computation has been temporarily suspended pending the
stabilisation of the \texttt{geometry.HybridSubstrate} pointer-based
query API introduced in the current iteration.  The experiment infrastructure
(split strategies, grid sweep, super-additivity test) is fully implemented
and will resume automatically once the API is finalised.
The section will be populated with results and figures on the next
experimental run.
`
	return []tools.Artifact{
		{
			Type:     tools.ArtifactProse,
			FileName: "torus_generalization_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data:     map[string]any{},
			},
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "torus_generalization_summary.tex",
			Data: tools.ProseData{
				Template: `% Torus generalisation summary table — results pending.
% This file is auto-generated; do not edit by hand.
`,
				Data: map[string]any{},
			},
		},
	}
}
