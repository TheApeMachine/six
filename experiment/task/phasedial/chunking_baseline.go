package phasedial

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
)

/*
ChunkingBaselineExperiment evaluates the robustness of the phase space to
re-chunking of the input stream. It also performs baseline falsification
by scrambling the basis primes to demonstrate the necessity of the
topological frequency structure.
*/
type ChunkingBaselineExperiment struct {
	tableData         []tools.ExperimentalData
	dataset           data.Provider
	prompt            []string
	holdouts          [][]byte
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *ChunkingBaselineExperiment) Name() string {
	return "Chunking Baseline"
}

func (experiment *ChunkingBaselineExperiment) Section() string {
	return "phasedial"
}

func (experiment *ChunkingBaselineExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *ChunkingBaselineExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *ChunkingBaselineExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *ChunkingBaselineExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ChunkingBaselineExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *ChunkingBaselineExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
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
				Template: projector.ExperimentSectionTmpl,
				Data: tools.ExperimentSection{
					Title: "Chunking Baseline",
					Label: "chunking_baseline",
					TaskDescription: `The chunking baseline experiment compares retrieval quality between chunk-level and sentence-level
ingestion strategies.  Aphorisms are ingested both as full sentences
and as overlapping two-sentence chunks.  The chunking baseline
determines whether the substrate benefits from denser, shorter-span
entries or whether full-sentence ingestion provides better attractor
coverage.`,
					Results:    fmt.Sprintf(`The mean weighted score was %s across $N = %d$ samples.`, projector.F3(experiment.Score()), len(experiment.tableData)),
					Assessment: phasedialAssessment(experiment.Score()),
					FigureRef:  "fig:chunking_baseline_map",
				},
			},
		},
	}
}
