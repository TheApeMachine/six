package misc

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
	"github.com/theapemachine/six/pkg/system/vm/input"
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
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    []string
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

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.3
			},
			Description: "The system demonstrates non-trivial secondary structure prediction from raw amino acid sequences.",
		},
	}

	return experiment
}

func (experiment *ProteinStructureExperiment) Name() string {
	return "ProteinStructure"
}

func (experiment *ProteinStructureExperiment) Section() string {
	return "misc"
}

func (experiment *ProteinStructureExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ProteinStructureExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *ProteinStructureExperiment) Holdout() (int, input.HoldoutType) {
	// Hold out the last 50 bytes for structure prediction
	return 50, input.RIGHT
}

/*
AddResult records an experimental observation.
*/
func (experiment *ProteinStructureExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ProteinStructureExperiment) Outcome() (any, gc.Assertion, any) {
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
	predBytes := best.Observed
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

	// ── Trial Outcome Map panels ───────────────────────────────────
	sampleLabels := make([]string, n)
	for i := range sampleLabels {
		sampleLabels[i] = fmt.Sprintf("S%d", i+1)
	}
	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}

	heatData := make([][]any, 0, n*4)
	for sIdx, row := range experiment.tableData {
		vals := []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal}
		for cIdx, v := range vals {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}
	}

	weightedPerSample := make([]float64, n)
	meanLine := make([]float64, n)
	for i, row := range experiment.tableData {
		weightedPerSample[i] = row.WeightedTotal
		meanLine[i] = score
	}

	panels := []tools.Panel{
		// ── Panel A: score fingerprint ─────────────────────────────
		{
			Kind:        "heatmap",
			Title:       "A: Score Fingerprint",
			GridLeft:    "4%",
			GridRight:   "72%",
			GridTop:     "12%",
			GridBottom:  "20%",
			XLabels:     scoreLabels,
			XAxisName:   "",
			XShow:       true,
			YLabels:     sampleLabels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     "27%",
		},
		// ── Panel B: weighted score per sample ─────────────────────
		{
			Kind:       "chart",
			Title:      "B: Weighted Score",
			GridLeft:   "30%",
			GridRight:  "52%",
			GridTop:    "12%",
			GridBottom: "20%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Weighted", Kind: "bar", BarWidth: "55%", Data: weightedPerSample},
				{
					Name:   fmt.Sprintf("Mean (%.2f)", score),
					Kind:   "dashed",
					Symbol: "none",
					Color:  "#f97316",
					Data:   meanLine,
				},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
		// ── Panel C: per-position alignment strip ──────────────────
		{
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
		},
	}

	// ── Prose template ─────────────────────────────────────────────
	proseTemplate := `\subsection{Protein Secondary Structure Prediction}
\label{sec:protein_structure}

\paragraph{Task Description.}
The protein secondary structure experiment evaluates whether the
geometric substrate can predict per-residue secondary structure
labels---Helix (\texttt{H}), Sheet (\texttt{E}), Coil
(\texttt{C})---from raw amino acid sequences, using solely the
bitwise chord resonance of the input characters.
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
non-trivial periodic structure in the amino acid byte stream.

\paragraph{Results.}
Figure~\ref{fig:protein_trial_map} shows the three-panel composite.
Panel~A is the score fingerprint heatmap across all $N = {{.N}}$ samples.
Panel~B shows the per-sample weighted score against the mean
({{.Score | f2}}, orange dashed line).
Panel~C is the per-position alignment strip for the highest-scoring
sample (S{{.BestIdx}}, weighted score {{.BestScore | f2}}):
rows show Truth (top) and Predicted (middle) labels encoded as
H$\to$1, E$\to$0.5, C$\to$0; the bottom row shows exact
position-level matches (1 = correct, 0 = incorrect).

The system achieved an exact-sequence accuracy of {{.ExactRate | pct}},
a mean partial score of {{.PartialRate | f3}}, and an overall
weighted score of {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate significantly exceeded the 3-class random baseline
($\approx 33\%$), suggesting that the non-commutative manifold
rotations naturally encode the periodic byte-level patterns
associated with $\alpha$-helical and $\beta$-sheet periodicity.
{{- else if gt .Score 0.2 -}}
\paragraph{Assessment.}
The weighted score exceeds zero but remains near the random
baseline.  Partial matches (Panel~A, Partial column) indicate that
the substrate recovers some structural regularity but fails to
maintain accurate label prediction over longer subsequences.
Increasing the ingestion sample count is expected to sharpen the
periodic attractors for each structural class.
{{- else -}}
\paragraph{Assessment.}
Performance is near the random baseline, indicating that the
current substrate size is insufficient to distinguish the three
structural attractor classes from raw amino acid byte patterns at
this resolution.
{{- end}}

`

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
				Template: proseTemplate,
				Data: map[string]any{
					"N":           n,
					"Score":       score,
					"ExactRate":   exactRate,
					"PartialRate": partialRate,
					"BestIdx":     bestIdx + 1,
					"BestScore":   best.WeightedTotal,
				},
			},
		},
	}
}
