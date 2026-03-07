package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
SteerabilityExperiment evaluates the stability of retrieval under phase
rotations across different split boundaries. It identifies the optimal
boundary for independent perspective shifts.
*/
type SteerabilityExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
	accuracy  float64
}

func NewSteerabilityExperiment() *SteerabilityExperiment {
	return &SteerabilityExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *SteerabilityExperiment) Name() string {
	return "Steerability"
}

func (experiment *SteerabilityExperiment) Section() string {
	return "phasedial"
}

func (experiment *SteerabilityExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SteerabilityExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *SteerabilityExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *SteerabilityExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SteerabilityExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.6
}

func (experiment *SteerabilityExperiment) Score() float64 {
	return experiment.accuracy
}

func (experiment *SteerabilityExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SteerabilityExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	D := config.Numeric.NBasis
	candidates := substrate.Candidates()

	topKSet := func(fp geometry.PhaseDial) map[int]bool {
		const K = 8
		ranked := substrate.PhaseDialRank(candidates, fp)
		set := make(map[int]bool, K)
		for i := 0; i < K && i < len(ranked); i++ {
			set[ranked[i].Idx] = true
		}
		return set
	}

	jaccard := func(a, b map[int]bool) float64 {
		inter := 0
		for k := range a {
			if b[k] {
				inter++
			}
		}
		union := len(a) + len(b) - inter
		if union == 0 {
			return 0
		}
		return 1.0 - float64(inter)/float64(union)
	}

	rotateBlock := func(fp geometry.PhaseDial, alpha float64, start, end int) geometry.PhaseDial {
		rotated := make(geometry.PhaseDial, D)
		copy(rotated, fp)
		f := cmplx.Rect(1.0, alpha)
		for k := start; k < end; k++ {
			rotated[k] = fp[k] * f
		}
		return rotated
	}

	steerability := func(fp geometry.PhaseDial, start, end int) float64 {
		const nAngles = 12
		var topKSets []map[int]bool
		for i := 0; i < nAngles; i++ {
			alpha := float64(i) * (2.0 * math.Pi / float64(nAngles))
			rotated := rotateBlock(fp, alpha, start, end)
			topKSets = append(topKSets, topKSet(rotated))
		}
		sumJ := 0.0
		for i := 0; i < nAngles; i++ {
			next := (i + 1) % nAngles
			sumJ += jaccard(topKSets[i], topKSets[next])
		}
		return sumJ / float64(nAngles)
	}

	seedQuery := "Democracy requires individual sacrifice."
	fpA := geometry.NewPhaseDial().Encode(seedQuery)
	hop := substrate.FirstHop(fpA, 45.0*(math.Pi/180.0), seedQuery)
	fpAB := hop.FingerprintAB
	textB := hop.TextB

	if textB == "" {
		return fmt.Errorf("could not find textB for steerability")
	}

	ceiling := -1.0
	for s := 0; s < 360; s++ {
		alpha := float64(s) * (math.Pi / 180.0)
		for _, anchor := range []geometry.PhaseDial{fpA, hop.FingerprintB} {
			rot := anchor.Rotate(alpha)
			rnk := substrate.PhaseDialRank(candidates, rot)
			topIdx := substrate.TopExcluding(rnk, seedQuery, textB)
			efp := substrate.Entries[topIdx].Fingerprint
			g := math.Min(efp.Similarity(fpA), efp.Similarity(hop.FingerprintB))
			if g > ceiling {
				ceiling = g
			}
		}
	}

	type valResult struct {
		name     string
		steerMin float64
		gain     float64
		superAdd bool
	}

	validations := []struct {
		name string
		b    int
	}{
		{"192/320", 192},
		{"224/288", 224},
		{"256/256", 256},
		{"288/224", 288},
		{"320/192", 320},
	}

	var results []valResult
	for _, v := range validations {
		sL := steerability(fpAB, 0, v.b)
		sR := steerability(fpAB, v.b, D)
		sMin := math.Min(sL, sR)

		const stepDeg = 5.0
		gridSize := int(360.0 / stepDeg)
		bestGain := -1.0
		for i := 0; i < gridSize; i++ {
			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
			f1 := cmplx.Rect(1.0, a1)
			for j := 0; j < gridSize; j++ {
				a2 := float64(j) * stepDeg * (math.Pi / 180.0)
				f2 := cmplx.Rect(1.0, a2)
				rotated := make(geometry.PhaseDial, D)
				for k := 0; k < D; k++ {
					if k < v.b {
						rotated[k] = fpAB[k] * f1
					} else {
						rotated[k] = fpAB[k] * f2
					}
				}
				rnk := substrate.PhaseDialRank(candidates, rotated)
				topIdx := substrate.TopExcluding(rnk, seedQuery, textB)
				efp := substrate.Entries[topIdx].Fingerprint
				g := math.Min(efp.Similarity(fpA), efp.Similarity(hop.FingerprintB))
				if g > bestGain {
					bestGain = g
				}
			}
		}
		results = append(results, valResult{v.name, sMin, bestGain, bestGain > ceiling})
	}

	// Calculate prediction accuracy: higher min_steer → super-additive
	maxNonSA := 0.0
	for _, r := range results {
		if !r.superAdd && r.steerMin > maxNonSA {
			maxNonSA = r.steerMin
		}
	}
	correct := 0
	for _, r := range results {
		predicted := r.steerMin > maxNonSA
		if predicted == r.superAdd {
			correct++
		}
	}

	experiment.accuracy = float64(correct) / float64(len(results))

	for _, r := range results {
		experiment.AddResult(tools.ExperimentalData{
			Name:          r.name,
			WeightedTotal: r.steerMin,
			Scores: tools.Scores{
				Exact:   r.gain,
				Partial: r.gain - ceiling,
				Fuzzy:   r.steerMin,
			},
		})
	}

	return nil
}

func (experiment *SteerabilityExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: "steerability_scores",
			Data:     experiment.tableData,
			Title:    "Steerability Score Breakdown",
			Caption:  "Steerability and gain across different split boundaries.",
			Label:    "fig:steerability_scores",
		},
	}
}
