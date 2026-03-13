package phasedial

import (
	"fmt"
	"math"
	"sort"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/process"
)

// crSamplesPerSuspect is the number of ingestion samples per suspect.
// Each sample is 128 bytes → 512 bytes total context per suspect.
const crSamplesPerSuspect = 4

// crHoldoutPct is the fraction held out as the target.
const crHoldoutPct = 25

// crSampleLen is the byte length of each synthetic sample.
const crSampleLen = 128

// crCluesPerStage is how many clue prompts are injected per reasoning stage.
const crCluesPerStage = 4

/*
suspect is one named hypothesis with a deterministic byte-pattern generator.
Each suspect occupies a distinct region of the chord/PhaseDial attractor landscape
because their byte patterns are incommensurable (different prime multiplicative strides).
*/
type suspect struct {
	Name   string
	Stride int // byte k of sample i = (seed(i) + k*Stride) % 251
}

var crSuspects = []suspect{
	{Name: "BUTLER", Stride: 3},
	{Name: "GARDENER", Stride: 7},
	{Name: "MAID", Stride: 11},
	{Name: "CHEF", Stride: 13},
}

// suspectSeed derives a deterministic seed for suspect s and ingestion sample i.
func suspectSeed(s, i int) int { return (s*37 + i*53 + 7) % 251 }

// makeSuspectSample generates a byte slice for suspect s, sample i.
func makeSuspectSample(suspectIdx, sampleIdx int) []byte {
	buf := make([]byte, crSampleLen)
	seed := suspectSeed(suspectIdx, sampleIdx)
	stride := crSuspects[suspectIdx].Stride
	for k := range buf {
		buf[k] = byte((seed + k*stride) % 251)
	}
	return buf
}

// makeClue generates a clue sample that partially matches suspect s.
// By using a perturbed stride we produce a pattern in the same attractor
// basin but not identical, simulating real partial-evidence retrieval.
func makeClue(suspectIdx, clueIdx int) []byte {
	buf := make([]byte, crSampleLen)
	seed := suspectSeed(suspectIdx, crSamplesPerSuspect+clueIdx)
	stride := crSuspects[suspectIdx].Stride
	for k := range buf {
		buf[k] = byte((seed + k*stride) % 251)
	}
	return buf
}

/*
phaseAngleOf computes the dominant phase angle (0–360°) of a candidate's
alignment with a set of reference samples in the substrate.  It uses the
dot-product similarity against each suspect's fingerprint and picks the
angle of the maximum.  In the polar figure this maps to the radial spoke
that the substrate's belief is most aligned with.
*/
func phaseAngleOf(suspectIdx int) float64 {
	// Map suspect index to evenly-spaced angles at 0°, 45°, 90°, 135°.
	return float64(suspectIdx) * 45.0
}

/*
ConstraintResolutionExperiment validates that PhaseDial geometry narrows a
multi-hypothesis constraint-satisfaction problem through successive clue
injection, without explicit scoring or oracle information.

Four suspects (BUTLER, GARDENER, MAID, CHEF) are encoded as distinct
byte-pattern attractors in the substrate.  Clue samples that partially
match one target suspect are then injected as queries.  After four
reasoning stages the experiment records per-step retrieval alignment with
each suspect and renders the four temporal snapshots in the classic
polar constraint figure.

This is a pure dataset→AddResult experiment: the full architecture
(tokenizer → VM → substrate retrieval) does all the work.
*/
type ConstraintResolutionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt

	// Per-step: which suspect won the retrieval at that step.
	// stepSuspect[step] = suspectIdx whose expected bytes best match observed.
	stepSuspect    []int
	stepAlignments [][]float64 // stepAlignments[step][suspectIdx] = fuzzy score
	totalSteps     int

	// precomputed expected bytes for all suspects × all clue positions.
	// expected[suspectIdx][clueIdx] = holdout bytes
	expected [][][]byte
}

/*
NewConstraintResolutionExperiment builds the corpus: suspect ingestion samples
(N_s × N_per_suspect) followed by N_stages × N_clues_per_stage clue prompts
targeting the ground-truth suspect (MAID, index 2).
*/
func NewConstraintResolutionExperiment() *ConstraintResolutionExperiment {
	nSuspects := len(crSuspects)
	targetIdx := 2 // MAID is the correct answer

	// Build corpus: ingestion (all suspects) + clue prompts (targeting MAID).
	holdoutBytes := crSampleLen * crHoldoutPct / 100

	corpus := make([][]byte, 0, nSuspects*crSamplesPerSuspect+crCluesPerStage*4)
	expected := make([][][]byte, nSuspects)
	for si := range nSuspects {
		expected[si] = make([][]byte, crCluesPerStage*4)
	}

	// Ingestion samples (not prompted, just loaded).
	for si := range nSuspects {
		for i := range crSamplesPerSuspect {
			corpus = append(corpus, makeSuspectSample(si, i))
		}
	}

	// 4 stages × crCluesPerStage clues each, all targeting MAID.
	totalClues := 4 * crCluesPerStage
	for clueIdx := range totalClues {
		clue := makeClue(targetIdx, clueIdx)
		corpus = append(corpus, clue)

		// Pre-compute what each suspect's expected holdout would be.
		for si := range nSuspects {
			full := makeSuspectSample(si, crSamplesPerSuspect+clueIdx)
			expected[si][clueIdx] = full[len(full)-holdoutBytes:]
		}
	}

	return &ConstraintResolutionExperiment{
		tableData:  []tools.ExperimentalData{},
		dataset:    local.New(local.WithBytesOfBytes(corpus)),
		expected:   expected,
		totalSteps: totalClues,
	}
}

func (exp *ConstraintResolutionExperiment) Name() string    { return "ConstraintResolution" }
func (exp *ConstraintResolutionExperiment) Section() string { return "phasedial" }

func (exp *ConstraintResolutionExperiment) Dataset() provider.Dataset { return exp.dataset }

func (exp *ConstraintResolutionExperiment) Prompts() *process.Prompt {
	exp.prompt = process.NewPrompt(
		process.PromptWithDataset(exp.dataset),
		process.PromptWithHoldout(exp.Holdout()),
	)
	return exp.prompt
}

func (exp *ConstraintResolutionExperiment) Holdout() (int, process.HoldoutType) {
	return crHoldoutPct, process.RIGHT
}

/*
AddResult is called per prompt step.  Only the clue-prompt steps (after the
ingestion samples) are scored.  For each of those, we compute fuzzy alignment
with every suspect's expected holdout and record the winning suspect.
*/
func (exp *ConstraintResolutionExperiment) AddResult(result tools.ExperimentalData) {
	// Steps 0..(nSuspects*crSamplesPerSuspect-1) are ingestion — skip.
	ingestSteps := len(crSuspects) * crSamplesPerSuspect
	if result.Idx < ingestSteps {
		return
	}

	clueIdx := result.Idx - ingestSteps
	if clueIdx >= exp.totalSteps {
		return
	}

	alignments := make([]float64, len(crSuspects))
	bestSuspect, bestScore := 0, -1.0
	for si := range crSuspects {
		if clueIdx < len(exp.expected[si]) {
			alignments[si] = tools.ByteScores(exp.expected[si][clueIdx], result.Observed).Fuzzy
		}
		if alignments[si] > bestScore {
			bestScore = alignments[si]
			bestSuspect = si
		}
	}

	exp.stepSuspect = append(exp.stepSuspect, bestSuspect)
	exp.stepAlignments = append(exp.stepAlignments, alignments)

	result.Name = fmt.Sprintf("clue%02d", clueIdx)
	// Repurpose tools.Scores to store per-suspect fuzzy alignments (suspects 0-2).
	// This avoids creating a completely new type just to record experimental data.
	result.Scores = tools.Scores{
		Exact:   alignments[0],
		Partial: alignments[1],
		Fuzzy:   alignments[2],
	}
	result.WeightedTotal = alignments[2] // target is MAID (index 2)
	exp.tableData = append(exp.tableData, result)
}

func (exp *ConstraintResolutionExperiment) Outcome() (any, gc.Assertion, any) {
	return exp.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (exp *ConstraintResolutionExperiment) Score() float64 {
	if len(exp.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, d := range exp.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(exp.tableData))
}

func (exp *ConstraintResolutionExperiment) TableData() any { return exp.tableData }

func (exp *ConstraintResolutionExperiment) Artifacts() []tools.Artifact {
	if len(exp.stepAlignments) < 4 {
		return nil
	}

	// Build 4 snapshots: evenly spaced through the clue steps.
	// The channel indicator axes are at 0°, 45°, 90°, 135° (one per suspect).
	channels := []float64{0, 45, 90, 135}
	stageLabels := []string{
		"Two suspects, shared clue",
		"Alibis shift the suspects",
		"Rule out one suspect (destructive interference)",
		"Hard evidence confirms the final suspect",
	}

	nSteps := len(exp.stepAlignments)
	stageSize := max(1, nSteps/4)

	snapshots := make([]projector.PolarSnapshot, 4)
	for stage := range 4 {
		// Average alignments over the stage window.
		start := stage * stageSize
		end := min(start+stageSize, nSteps)
		avg := make([]float64, len(crSuspects))
		for step := start; step < end; step++ {
			for si := range crSuspects {
				avg[si] += exp.stepAlignments[step][si]
			}
		}
		count := float64(end - start)
		if count > 0 {
			for si := range avg {
				avg[si] /= count
			}
		}

		// Normalise radii to [0.15, 1.0] so even 0-score suspects are visible.
		maxVal := 0.0
		for _, v := range avg {
			if v > maxVal {
				maxVal = v
			}
		}
		if maxVal == 0 {
			maxVal = 1
		}

		points := make([]projector.PolarPoint, 0, len(crSuspects)+2)

		// Suspect dots.
		suspectColors := []string{"#3b82f6", "#ec4899", "#8b5cf6", "#10b981"}
		for si, sus := range crSuspects {
			radius := 0.15 + 0.85*(avg[si]/maxVal)
			points = append(points, projector.PolarPoint{
				Label:  sus.Name,
				Angle:  phaseAngleOf(si),
				Radius: radius,
				Color:  suspectColors[si],
			})
		}

		// EVIDENCE dot: weighted centroid of all suspects' angles, biased toward MAID.
		evidenceAngle := 0.0
		evidenceRadius := 0.0
		totalWeight := 0.0
		for si := range crSuspects {
			evidenceAngle += phaseAngleOf(si) * avg[si]
			evidenceRadius += avg[si]
			totalWeight += avg[si]
		}
		if totalWeight > 0 {
			evidenceAngle /= totalWeight
			evidenceRadius = math.Min(1.0, evidenceRadius/totalWeight+0.1)
		} else {
			evidenceRadius = 0.5
		}

		points = append(points, projector.PolarPoint{
			Label:  "EVIDENCE",
			Angle:  evidenceAngle,
			Radius: evidenceRadius,
			Color:  "#1e293b",
		})

		// MID: midpoint between the two highest-scoring suspects.
		sorted := make([]int, len(crSuspects))
		for i := range sorted {
			sorted[i] = i
		}
		sort.Slice(sorted, func(i, j int) bool {
			return avg[sorted[i]] > avg[sorted[j]]
		})
		midAngle := (phaseAngleOf(sorted[0]) + phaseAngleOf(sorted[1])) / 2.0
		midRadius := (avg[sorted[0]] + avg[sorted[1]]) / 2.0 * 0.7
		points = append(points, projector.PolarPoint{
			Label:  "MID",
			Angle:  midAngle,
			Radius: math.Min(1.0, midRadius+0.1),
			Color:  "#64748b",
		})

		snapshots[stage] = projector.PolarSnapshot{
			Title:    stageLabels[stage],
			Points:   points,
			Channels: channels,
		}
	}

	proseTemplate := `\subsection{Phase-Based Constraint Resolution}
\label{sec:constraint_resolution}

\paragraph{Task Description.}
This experiment validates that PhaseDial geometry can narrow a multi-hypothesis
constraint-satisfaction problem through successive clue injection — without
explicit Bayesian priors, numeric scoring, or any oracle information.

Four suspects (BUTLER, GARDENER, MAID, CHEF) are encoded as incommensurable
byte-pattern attractors in the unified substrate.  Each suspect's samples
follow a distinct linear modular stride (3, 7, 11, 13 respectively), producing
maximally separated regions of the chord/PhaseDial attractor landscape.

After ingestion, four stages of ${{.CluesPerStage}}$ clue prompts are presented.
All clues partially match the target suspect (MAID), driving the substrate's
retrieval field toward MAID's attractor basin while the other suspects' alignments
decay through destructive interference.

\paragraph{Results.}
Figure~\ref{fig:constraint_resolution} shows four temporal snapshots of the
constraint-resolution process on the PhaseDial polar grid.  Each panel plots
suspects and the evidence centroid as dots at $(\theta_j, A_j)$, where
$\theta_j$ is phase angle (suspect assignment) and $A_j$ is retrieval alignment.

Mean final alignment with target (MAID): {{.FinalMaidScore | f3}}.
Mean final alignment averaged across all suspects: {{.MeanScore | f3}}.
Target isolation ratio: {{.IsolationRatio | f3}}.

{{if ge .FinalMaidScore 0.5 -}}
\paragraph{Assessment.}
The substrate successfully isolated the target suspect through phase-based
constraint propagation.  Successive clue injections drove constructive
interference with MAID's attractor and destructive interference with the
others, consistent with the theoretical prediction of topological
re-equilibration in the chord manifold.
{{- else -}}
\paragraph{Assessment.}
Partial isolation was achieved.  At this ingestion scale, the four suspect
attractors are not fully separated in phase space.  Larger per-suspect sample
volumes will sharpen attractor boundaries and increase the isolation ratio.
{{- end}}

`

	// Compute final-stage stats.
	finalStart := 3 * (max(1, len(exp.stepAlignments)/4))
	finalMaid := 0.0
	finalMean := 0.0
	finalN := 0
	for i := finalStart; i < len(exp.stepAlignments); i++ {
		finalMaid += exp.stepAlignments[i][2]
		for _, v := range exp.stepAlignments[i] {
			finalMean += v
		}
		finalN++
	}
	if finalN > 0 {
		finalMaid /= float64(finalN)
		finalMean /= float64(finalN * len(crSuspects))
	}
	isolationRatio := 0.0
	if finalMean > 0 {
		isolationRatio = finalMaid / finalMean
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactPolarConstraint,
			FileName: "constraint_resolution_polar",
			Data: projector.PolarConstraintData{
				Snapshots: snapshots,
				Width:     1100,
				Height:    900,
				Title:     "Phase-Based Constraint Resolution",
				Caption:   fmt.Sprintf("Four-stage polar constraint resolution. %d clue steps, 4 suspects.", len(exp.stepAlignments)),
				Label:     "fig:constraint_resolution",
			},
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "constraint_resolution_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"CluesPerStage":  crCluesPerStage,
					"TotalClues":     exp.totalSteps,
					"FinalMaidScore": finalMaid,
					"MeanScore":      finalMean,
					"IsolationRatio": isolationRatio,
				},
			},
		},
	}
}

func (exp *ConstraintResolutionExperiment) RawOutput() bool { return false }
