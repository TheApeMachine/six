package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
SteerabilityExperiment evaluates the stability of retrieval under phase
rotations across different split boundaries. It identifies the optimal
boundary for independent perspective shifts.
*/
type SteerabilityExperiment struct {
	tableData       []tools.ExperimentalData
	dataset         provider.Dataset
	prompt          []string
	accuracy        float64
	splitCandidates []int
	sweepStepDeg    float64
}

type steerabilityOpt func(*SteerabilityExperiment)

func NewSteerabilityExperiment(opts ...steerabilityOpt) *SteerabilityExperiment {
	experiment := &SteerabilityExperiment{
		tableData:       []tools.ExperimentalData{},
		dataset:         tools.NewLocalProvider(tools.Aphorisms),
		splitCandidates: []int{192, 224, 256, 288, 320},
		sweepStepDeg:    5.0,
	}

	for _, opt := range opts {
		opt(experiment)
	}

	if len(experiment.splitCandidates) == 0 {
		experiment.splitCandidates = []int{192, 224, 256, 288, 320}
	}

	if experiment.sweepStepDeg <= 0 || experiment.sweepStepDeg > 180 {
		experiment.sweepStepDeg = 5.0
	}

	return experiment
}

func SteerabilityWithDataset(dataset provider.Dataset) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if dataset != nil {
			experiment.dataset = dataset
		}
	}
}

func SteerabilityWithSplitCandidates(splitCandidates []int) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if len(splitCandidates) > 0 {
			experiment.splitCandidates = append([]int(nil), splitCandidates...)
		}
	}
}

func SteerabilityWithSweepStep(stepDeg float64) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if stepDeg > 0 && stepDeg <= 180 {
			experiment.sweepStepDeg = stepDeg
		}
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

func (experiment *SteerabilityExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *SteerabilityExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
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

func (experiment *SteerabilityExperiment) RawOutput() bool { return false }

// func (experiment *SteerabilityExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
// 	D := config.Numeric.NBasis
// 	candidates := substrate.Candidates()

// 	topKSet := func(fp geometry.PhaseDial) map[int]bool {
// 		const K = 8
// 		ranked := substrate.PhaseDialRank(candidates, fp)
// 		set := make(map[int]bool, K)
// 		for i := 0; i < K && i < len(ranked); i++ {
// 			set[ranked[i].Idx] = true
// 		}
// 		return set
// 	}

// 	jaccard := func(a, b map[int]bool) float64 {
// 		inter := 0
// 		for k := range a {
// 			if b[k] {
// 				inter++
// 			}
// 		}
// 		union := len(a) + len(b) - inter
// 		if union == 0 {
// 			return 0
// 		}
// 		return 1.0 - float64(inter)/float64(union)
// 	}

// 	rotateBlock := func(fp geometry.PhaseDial, alpha float64, start, end int) geometry.PhaseDial {
// 		rotated := make(geometry.PhaseDial, D)
// 		copy(rotated, fp)
// 		f := cmplx.Rect(1.0, alpha)
// 		for k := start; k < end; k++ {
// 			rotated[k] = fp[k] * f
// 		}
// 		return rotated
// 	}

// 	steerability := func(fp geometry.PhaseDial, start, end int) float64 {
// 		const nAngles = 12
// 		var topKSets []map[int]bool
// 		for i := 0; i < nAngles; i++ {
// 			alpha := float64(i) * (2.0 * math.Pi / float64(nAngles))
// 			rotated := rotateBlock(fp, alpha, start, end)
// 			topKSets = append(topKSets, topKSet(rotated))
// 		}
// 		sumJ := 0.0
// 		for i := 0; i < nAngles; i++ {
// 			next := (i + 1) % nAngles
// 			sumJ += jaccard(topKSets[i], topKSets[next])
// 		}
// 		return sumJ / float64(nAngles)
// 	}

// 	seedQueryChords := substrate.Entries[0].Readout
// 	fpA := substrate.Entries[0].Fingerprint
// 	hop := substrate.FirstHop(fpA, 45.0*(math.Pi/180.0), seedQueryChords)
// 	fpAB := hop.FingerprintAB
// 	readoutB := hop.ReadoutB

// 	if len(readoutB) == 0 {
// 		return fmt.Errorf("could not find readoutB for steerability")
// 	}

// 	ceiling := -1.0
// 	for s := 0; s < 360; s++ {
// 		alpha := float64(s) * (math.Pi / 180.0)
// 		for _, anchor := range []geometry.PhaseDial{fpA, hop.FingerprintB} {
// 			rot := anchor.Rotate(alpha)
// 			rnk := substrate.PhaseDialRank(candidates, rot)
// 			topIdx := substrate.TopExcluding(rnk, seedQueryChords, readoutB)
// 			efp := substrate.Entries[topIdx].Fingerprint
// 			g := math.Min(efp.Similarity(fpA), efp.Similarity(hop.FingerprintB))
// 			if g > ceiling {
// 				ceiling = g
// 			}
// 		}
// 	}

// 	type valResult struct {
// 		name     string
// 		steerMin float64
// 		gain     float64
// 		superAdd bool
// 	}

// 	validations := []struct {
// 		name string
// 		b    int
// 	}{}

// 	seen := make(map[int]bool, len(experiment.splitCandidates))
// 	for _, splitCandidate := range experiment.splitCandidates {
// 		if splitCandidate < 1 || splitCandidate >= D {
// 			continue
// 		}

// 		if seen[splitCandidate] {
// 			continue
// 		}

// 		seen[splitCandidate] = true
// 		validations = append(validations, struct {
// 			name string
// 			b    int
// 		}{
// 			name: fmt.Sprintf("%d/%d", splitCandidate, D-splitCandidate),
// 			b:    splitCandidate,
// 		})
// 	}

// 	if len(validations) == 0 {
// 		half := D / 2
// 		validations = append(validations, struct {
// 			name string
// 			b    int
// 		}{
// 			name: fmt.Sprintf("%d/%d", half, D-half),
// 			b:    half,
// 		})
// 	}

// 	var results []valResult
// 	for _, v := range validations {
// 		sL := steerability(fpAB, 0, v.b)
// 		sR := steerability(fpAB, v.b, D)
// 		sMin := math.Min(sL, sR)

// 		stepDeg := experiment.sweepStepDeg
// 		gridSize := int(360.0 / stepDeg)
// 		bestGain := -1.0
// 		for i := 0; i < gridSize; i++ {
// 			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
// 			f1 := cmplx.Rect(1.0, a1)
// 			for j := 0; j < gridSize; j++ {
// 				a2 := float64(j) * stepDeg * (math.Pi / 180.0)
// 				f2 := cmplx.Rect(1.0, a2)
// 				rotated := make(geometry.PhaseDial, D)
// 				for k := 0; k < D; k++ {
// 					if k < v.b {
// 						rotated[k] = fpAB[k] * f1
// 					} else {
// 						rotated[k] = fpAB[k] * f2
// 					}
// 				}
// 				rnk := substrate.PhaseDialRank(candidates, rotated)
// 				topIdx := substrate.TopExcluding(rnk, seedQueryChords, readoutB)
// 				efp := substrate.Entries[topIdx].Fingerprint
// 				g := math.Min(efp.Similarity(fpA), efp.Similarity(hop.FingerprintB))
// 				if g > bestGain {
// 					bestGain = g
// 				}
// 			}
// 		}
// 		results = append(results, valResult{v.name, sMin, bestGain, bestGain > ceiling})
// 	}

// 	// Calculate prediction accuracy: higher min_steer → super-additive
// 	maxNonSA := 0.0
// 	for _, r := range results {
// 		if !r.superAdd && r.steerMin > maxNonSA {
// 			maxNonSA = r.steerMin
// 		}
// 	}
// 	correct := 0
// 	for _, r := range results {
// 		predicted := r.steerMin > maxNonSA
// 		if predicted == r.superAdd {
// 			correct++
// 		}
// 	}

// 	experiment.accuracy = float64(correct) / float64(len(results))

// 	for _, r := range results {
// 		experiment.AddResult(tools.ExperimentalData{
// 			Name:          r.name,
// 			WeightedTotal: r.steerMin,
// 			Scores: tools.Scores{
// 				Exact:   r.gain,
// 				Partial: r.gain - ceiling,
// 				Fuzzy:   r.steerMin,
// 			},
// 		})
// 	}

// 	return nil
// }

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
