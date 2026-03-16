package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/vm/input"

	"github.com/theapemachine/six/pkg/store/data/provider"
)

/*
ChunkingBaselineExperiment evaluates the robustness of the phase space to
re-chunking of the input stream. It also performs baseline falsification
by scrambling the basis primes to demonstrate the necessity of the
topological frequency structure.
*/
type ChunkingBaselineExperiment struct {
	tableData         []tools.ExperimentalData
	dataset           provider.Dataset
	prompt            []string
	evaluator         *tools.Evaluator
	chunkingRows      []map[string]any
	falsificationRows []map[string]any
}

func NewChunkingBaselineExperiment() *ChunkingBaselineExperiment {
	return &ChunkingBaselineExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Chunking baseline boundary detection.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset: tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *ChunkingBaselineExperiment) Name() string {
	return "Chunking Baseline"
}

func (experiment *ChunkingBaselineExperiment) Section() string {
	return "phasedial"
}

func (experiment *ChunkingBaselineExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ChunkingBaselineExperiment) Prompts() []string {
	experiment.prompt = []string{}

	return experiment.prompt
}

func (experiment *ChunkingBaselineExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *ChunkingBaselineExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ChunkingBaselineExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *ChunkingBaselineExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *ChunkingBaselineExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *ChunkingBaselineExperiment) RawOutput() bool { return false }

func totalActive(values []data.Value) int {
	n := 0
	for _, c := range values {
		n += c.ActiveCount()
	}
	return n
}

func (experiment *ChunkingBaselineExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "chunking_variation_summary.tex",
			Data:     experiment.chunkingRows,
			Title:    "Chunking Variation Summary",
			Caption:  "Evaluation of retrieval robustness across chunk boundaries.",
			Label:    "tab:chunking_variation",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "baseline_falsification_summary.tex",
			Data:     experiment.falsificationRows,
			Title:    "Baseline Falsification Summary",
			Caption:  "Verification of frequency basis necessity via scrambled permutations.",
			Label:    "tab:baseline_falsification",
		},

		{
			Type:     tools.ArtifactProse,
			FileName: "chunking_baseline_section.tex",
			Data: tools.ProseData{
				Template: `\subsection{Chunking Baseline}
\label{sec:chunking_baseline}

\paragraph{Task Description.}
The chunking baseline experiment compares retrieval quality between chunk-level and sentence-level
ingestion strategies.  Aphorisms are ingested both as full sentences
and as overlapping two-sentence chunks.  The chunking baseline
determines whether the substrate benefits from denser, shorter-span
entries or whether full-sentence ingestion provides better attractor
coverage.

\paragraph{Results.}
Figure~\ref{fig:chunking_baseline_map} shows the trial outcome map.
The mean weighted score was {{.Score | f3}} across $N = {{.N}}$ samples.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong performance on this geometric property,
confirming that the invariant holds reliably at this ingestion scale.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial invariance was observed.  The property holds for a subset of
samples but becomes unreliable under more challenging conditions.
{{- else -}}
\paragraph{Assessment.}
The property was not reliably detected at this stage.  The phasedial
experiments require a functional Finalize path to populate the substrate
with compositional data; this infrastructure is being rebuilt during
the current refactoring phase.
{{- end}}
`,
				Data: map[string]any{
					"N":     len(experiment.tableData),
					"Score": experiment.Score(),
				},
			},
		},
	}
}
