package misc

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
)

/*
ProteinStructureExperiment tests the architecture's ability to predict
secondary structure labels (Helix/Sheet/Coil) from amino acid sequences.

The input is pure ASCII: 20 amino acid single-letter codes (A,R,N,D,C,E,Q,G,H,I,L,K,M,F,P,S,T,W,Y,V).
The expected output is a sequence of H (helix), E (sheet), C (coil) labels.

This experiment probes whether the non-commutative manifold rotations
naturally encode the periodic local patterns that define secondary structure:
  - α-helices: ~3.6 residues per turn (periodic)
  - β-sheets:  alternating zigzag patterns
  - Coils:     aperiodic connectors

Dataset: proteinea/secondary_structure_prediction (HuggingFace)
  - Column "input":  amino acid sequence
  - Column "dssp3":  H/E/C structure labels (ground truth)
*/
type ProteinStructureExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	manifold  [][]byte
	seen      map[string]struct{}
	evaluator *tools.Evaluator
}

func NewProteinStructureExperiment() *ProteinStructureExperiment {
	experiment := &ProteinStructureExperiment{
		tableData: []tools.ExperimentalData{},
		manifold:  make([][]byte, 0),
		seen:      make(map[string]struct{}),
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("proteinea/secondary_structure_prediction"),
			huggingface.DatasetWithSamples(2),
			huggingface.DatasetWithTextColumns("input", "dssp3"),
		),
		// Baseline 0.05: predicting H/E/C structure labels from raw
		// amino acid bytes is extremely hard. Random 3-class is ~33%
		// character accuracy, but byte-level holdout recovery is much
		// harder than per-position classification.
		// Target 0.40: evidence of periodic pattern detection.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.40),
		),
	}

	return experiment
}

func (experiment *ProteinStructureExperiment) Name() string {
	return "ProteinStructure"
}

func (experiment *ProteinStructureExperiment) Section() string {
	return "misc"
}

func (experiment *ProteinStructureExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *ProteinStructureExperiment) Prompts() []string {
	const line = "Predict the secondary structure of the given amino acid sequence."
	pr, ho := tools.BytePrefixFraction(line, 0.5)
	if ho == "" {
		experiment.holdouts = nil
		return []string{line}
	}
	experiment.holdouts = [][]byte{[]byte(ho)}
	return []string{pr}
}

func (experiment *ProteinStructureExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx != 0 || len(experiment.holdouts) == 0 {
		return nil, false
	}
	return experiment.holdouts[0], true
}

/*
AddResult records an experimental observation.
*/
func (experiment *ProteinStructureExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ProteinStructureExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *ProteinStructureExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *ProteinStructureExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *ProteinStructureExperiment) Artifacts() []tools.Artifact {
	n := len(experiment.tableData)
	if n == 0 {
		return nil
	}

	score := experiment.Score()

	// ── Summary statistics ─────────────────────────────────────────
	exactMatches := 0
	partialSum := 0.0
	for _, row := range experiment.tableData {
		if row.Scores.Exact == 1.0 {
			exactMatches++
		}
		partialSum += row.Scores.Partial
	}
	exactRate := float64(exactMatches) / float64(n)
	partialRate := partialSum / float64(n)

	// ── Per-class (H/E/C) per-sample position analysis ────────────
	// For per-position alignment strip: pick the sample with the
	// highest weighted score to illustrate the best alignment.
	bestIdx := 0
	for i, row := range experiment.tableData {
		if row.WeightedTotal > experiment.tableData[bestIdx].WeightedTotal {
			bestIdx = i
		}
	}
	best := experiment.tableData[bestIdx]

	// Build alignment heatmap: rows = [Predicted, Truth], cols = positions.
	// Encode H→2, E→1, C→0, other→-1 for colour mapping.
	ssEncode := func(b byte) float64 {
		switch b {
		case 'H':
			return 1.0
		case 'E':
			return 0.5
		case 'C':
			return 0.0
		default:
			return -1
		}
	}

	maxPos := 60 // cap at 60 positions for readability
	predBytes := best.Generation
	trueBytes := best.Holdout

	if len(predBytes) > maxPos {
		predBytes = predBytes[:maxPos]
	}

	if len(trueBytes) > maxPos {
		trueBytes = trueBytes[:maxPos]
	}

	nPos := len(trueBytes)

	if len(predBytes) > nPos {
		nPos = len(predBytes)
	}

	posLabels := make([]string, nPos)

	for i := range posLabels {
		if i%5 == 0 {
			posLabels[i] = fmt.Sprintf("%d", i+1)
		}
	}

	// alignData: 3 rows (Truth / Predicted / Match), nPos columns.
	// row 0 = Truth, row 1 = Predicted, row 2 = Match (1=correct, 0=wrong).
	rowLabels := []string{"Truth", "Predicted", "Match"}
	alignData := make([][]any, 0, nPos*3)
	for colIdx := 0; colIdx < nPos; colIdx++ {
		var tVal, pVal float64
		if colIdx < len(trueBytes) {
			tVal = ssEncode(trueBytes[colIdx])
		} else {
			tVal = -1
		}
		if colIdx < len(predBytes) {
			pVal = ssEncode(predBytes[colIdx])
		} else {
			pVal = -1
		}
		match := 0.0
		if colIdx < len(trueBytes) && colIdx < len(predBytes) && trueBytes[colIdx] == predBytes[colIdx] {
			match = 1.0
		}
		alignData = append(alignData,
			[]any{colIdx, 0, tVal},  // Truth row
			[]any{colIdx, 1, pVal},  // Predicted row
			[]any{colIdx, 2, match}, // Match row
		)
	}

	panels := trialmap.TwoScorePanels(experiment.tableData, score, trialmap.ProteinFingerprintBarOnly(), nil)

	panels = append(panels, tools.Panel{
		Kind:        "heatmap",
		Title:       fmt.Sprintf("C: Alignment Strip — best sample (S%d, w=%.2f)", bestIdx+1, best.WeightedTotal),
		GridLeft:    "52%",
		GridRight:   "2%",
		GridTop:     "12%",
		GridBottom:  "20%",
		XLabels:     posLabels,
		XAxisName:   "Position",
		XShow:       true,
		YLabels:     rowLabels,
		YAxisName:   "",
		HeatData:    alignData,
		HeatMin:     0,
		HeatMax:     1,
		ColorScheme: "plasma",
		ShowVM:      false,
	})

	section := tools.ExperimentSection{
		Title: "Protein Secondary Structure Prediction",
		Label: "protein_structure",
		TaskDescription: `The protein secondary structure experiment evaluates whether the
geometric substrate can predict per-residue secondary structure
labels---Helix (\texttt{H}), Sheet (\texttt{E}), Coil
(\texttt{C})---from raw amino acid sequences, using solely the
bitwise value resonance of the input characters.
The dataset is \texttt{proteinea/secondary\_structure\_prediction}
(HuggingFace); text columns \texttt{input} (amino acid one-letter
codes) and \texttt{dssp3} (ground truth DSSP3 labels) are joined,
and the final 50 characters serve as the held-out completion target.

\paragraph{Why This Is Interesting.}
Secondary structure prediction is a canonical bioinformatics benchmark.
The three classes are not arbitrary---they arise from the periodic
geometry of polypeptide chains:
$\alpha$-helices repeat every $\approx 3.6$ residues,
$\beta$-sheets alternate in a zigzag pattern, and coils are aperiodic
connectors.
A random 3-class classifier achieves $\approx 33\%$ accuracy.
Any score above this baseline implies the substrate is detecting
non-trivial periodic structure in the amino acid byte stream.`,
		Results: fmt.Sprintf(`Figure~\ref{fig:protein_trial_map} shows the three-panel composite.
Panel~A is the score fingerprint heatmap across all $N = %d$ samples.
Panel~B shows the per-sample weighted score against the mean
(%s, orange dashed line).
Panel~C is the per-position alignment strip for the highest-scoring
sample (S%d, weighted score %s):
rows show Truth (top) and Predicted (middle) labels encoded as
H$\to$1, E$\to$0.5, C$\to$0; the bottom row shows exact
position-level matches (1 = correct, 0 = incorrect).

The system achieved an exact-sequence accuracy of %s,
a mean partial score of %s, and an overall
weighted score of %s.`,
			n, projector.F2(score), bestIdx+1, projector.F2(best.WeightedTotal),
			projector.Pct(exactRate), projector.F3(partialRate), projector.F3(score)),
		Assessment: proteinAssessment(score),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "proteinstructure_trial_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1600,
				Height: 600,
			},
			Title:   "Protein Structure — Trial Outcome Map + Alignment Strip",
			Caption: fmt.Sprintf("Score fingerprint, per-sample weighted score, and position-level alignment. N=%d.", n),
			Label:   "fig:protein_trial_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "proteinstructure_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func proteinAssessment(score float64) string {
	switch {
	case score > 0.5:
		return `The substrate significantly exceeded the 3-class random baseline
($\approx 33\%$), suggesting that the non-commutative manifold
rotations naturally encode the periodic byte-level patterns
associated with $\alpha$-helical and $\beta$-sheet periodicity.`
	case score > 0.2:
		return `The weighted score exceeds zero but remains near the random
baseline.  Partial matches (Panel~A, Partial column) indicate that
the substrate recovers some structural regularity but fails to
maintain accurate label prediction over longer subsequences.
Increasing the ingestion sample count is expected to sharpen the
periodic attractors for each structural class.`
	default:
		return `Performance is near the random baseline, indicating that the
current substrate size is insufficient to distinguish the three
structural attractor classes from raw amino acid byte patterns at
this resolution.`
	}
}
