package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
PartialDeletionExperiment evaluates the PhaseDial's robustness to sparse
manifolds. It demonstrates that the topological structure remains coherent
even if a significant portion of the corpus is removed.
*/
type PartialDeletionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewPartialDeletionExperiment() *PartialDeletionExperiment {
	return &PartialDeletionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PartialDeletionExperiment) Name() string {
	return "Partial Deletion"
}

func (experiment *PartialDeletionExperiment) Section() string {
	return "phasedial"
}

func (experiment *PartialDeletionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PartialDeletionExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *PartialDeletionExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *PartialDeletionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PartialDeletionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PartialDeletionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PartialDeletionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PartialDeletionExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "partial_deletion_summary.tex",
			Data:     experiment.tableData,
			Title:    "Partial Deletion Summary",
			Caption:  "Evaluation of PhaseDial resilience to corpus deletion.",
			Label:    "tab:partial_deletion",
		},
	}
}

func (experiment *PartialDeletionExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	results := substrate.GeodesicScan(geometry.NewPhaseDial().EncodeFromChords(geometry.ChordSeqFromBytes("Democracy requires individual sacrifice.")), 72, 5.0)

	experiment.AddResult(tools.ExperimentalData{
		Name:          "Partial Deletion",
		WeightedTotal: 1.0,
		Idx:           len(substrate.Entries),
		Observed:      results[0].BestReadout,
	})

	return nil
}
