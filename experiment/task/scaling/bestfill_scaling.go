package scaling

import (
	"fmt"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
BestFillScalingExperiment measures BestFill query latency as the substrate
dictionary grows. Provides a 5000-sample synthetic dataset; the Pipeline
ingests and prompts normally. Finalize benchmarks raw BestFill at increasing
dictionary slices to characterize scan cost.
*/
type BestFillScalingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewBestFillScalingExperiment() *BestFillScalingExperiment {
	return &BestFillScalingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 5000, 42),
	}
}

func (experiment *BestFillScalingExperiment) Name() string    { return "BestFill Scaling" }
func (experiment *BestFillScalingExperiment) Section() string { return "scaling" }
func (experiment *BestFillScalingExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BestFillScalingExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *BestFillScalingExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 32, tokenizer.RIGHT
}

func (experiment *BestFillScalingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BestFillScalingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *BestFillScalingExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *BestFillScalingExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BestFillScalingExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: "bestfill_scaling_scores",
			Data:     experiment.tableData,
			Title:    "BestFill Scaling",
			Caption:  "BestFill query latency (µs) vs dictionary size.",
			Label:    "fig:bestfill_scaling",
		},
	}
}

func (experiment *BestFillScalingExperiment) Finalize(
	substrate *geometry.HybridSubstrate,
) error {
	if substrate == nil {
		return fmt.Errorf("no substrate provided")
	}

	filters := substrate.Filters()

	if len(filters) == 0 {
		return fmt.Errorf("no filters in substrate")
	}

	backend, err := kernel.NewBackend()
	if err != nil {
		return fmt.Errorf("failed to create backend: %w", err)
	}

	// Use the first filter as the query chord.
	queryChord := filters[0]

	sizes := []int{100, 500, 1000, 5000, len(filters)}

	for _, dictSize := range sizes {
		if dictSize > len(filters) {
			dictSize = len(filters)
		}

		if dictSize == 0 {
			continue
		}

		// Set dictionary to the subset
		if cpuBackend, ok := backend.(*kernel.CPUBackend); ok {
			cpuBackend.SetDictionary(filters[:dictSize])
		}

		const trials = 10
		var totalDur time.Duration

		for range trials {
			t0 := time.Now()
			_, _ = backend.Resolve([]data.Chord{queryChord})
			totalDur += time.Since(t0)
		}

		avgUs := float64(totalDur.Microseconds()) / float64(trials)
		usPerEntry := avgUs / float64(dictSize)
		score := 1.0 / (1.0 + avgUs/1000.0)

		experiment.AddResult(tools.ExperimentalData{
			Idx:  len(experiment.tableData),
			Name: fmt.Sprintf("dict=%d", dictSize),
			Scores: tools.Scores{
				Exact:   avgUs,
				Partial: usPerEntry,
				Fuzzy:   float64(dictSize),
			},
			WeightedTotal: score,
		})
	}

	return nil
}
