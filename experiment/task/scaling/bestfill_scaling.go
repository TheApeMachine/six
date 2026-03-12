package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/provider"
)

/*
BestFillScalingExperiment measures BestFill query latency as the substrate
dictionary grows. Provides a 100-sample synthetic dataset; the Pipeline
ingests normally. Finalize benchmarks raw BestFill at increasing dictionary
slices to characterise scan cost.

Note: modest sample count so the ingestion phase fits within the test
timeout. The paper prose notes that latency curves are expected to steepen
linearly with N.
*/
type BestFillScalingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewBestFillScalingExperiment() *BestFillScalingExperiment {
	return &BestFillScalingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 100, 42),
	}
}

func (experiment *BestFillScalingExperiment) Name() string    { return "BestFill Scaling" }
func (experiment *BestFillScalingExperiment) Section() string { return "scaling" }
func (experiment *BestFillScalingExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BestFillScalingExperiment) Prompts() *process.Prompt {
	// The substrate is populated during Loader.Start()
	// We don't need to run 5000 prompts through the active inference Cortex
	// just to benchmark the latency of raw BestFill.
	return nil
}

func (experiment *BestFillScalingExperiment) Holdout() (int, process.HoldoutType) {
	return 32, process.RIGHT
}

func (experiment *BestFillScalingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BestFillScalingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
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
	return BestFillArtifacts(experiment.tableData)
}

func (experiment *BestFillScalingExperiment) RawOutput() bool { return false }

// func (experiment *BestFillScalingExperiment) Finalize(substrate any) error {
// 	if substrate == nil {
// 		return fmt.Errorf("no substrate provided")
// 	}

// 	filters := substrate.Filters()

// 	if len(filters) == 0 {
// 		return fmt.Errorf("no filters in substrate")
// 	}

// 	// TODO: Replace with new pointer-based API
// 	/*
// 		builder := kernel.NewBuilder(kernel.WithBackend(&metal.MetalBackend{}))
// 		if err != nil {
// 			return fmt.Errorf("failed to create backend: %w", err)
// 		}
// 	*/

// 	// Use the first filter as the query chord.
// 	// queryChord := filters[0]

// 	sizes := []int{100, 500, 1000, 5000, len(filters)}

// 	for _, dictSize := range sizes {
// 		if dictSize > len(filters) {
// 			dictSize = len(filters)
// 		}

// 		if dictSize == 0 {
// 			continue
// 		}

// 		// Set dictionary to the subset
// 		/*
// 			if cpuBackend, ok := backend.(*kernel.CPUBackend); ok {
// 				cpuBackend.SetDictionary(filters[:dictSize])
// 			}
// 		*/

// 		const trials = 10
// 		var totalDur time.Duration

// 		/*
// 			for range trials {
// 				t0 := time.Now()
// 				_, _ = backend.Resolve([]data.Chord{queryChord})
// 				totalDur += time.Since(t0)
// 			}
// 		*/

// 		avgUs := float64(totalDur.Microseconds()) / float64(trials)
// 		usPerEntry := avgUs / float64(dictSize)
// 		score := 1.0 / (1.0 + avgUs/1000.0)

// 		experiment.AddResult(tools.ExperimentalData{
// 			Idx:  len(experiment.tableData),
// 			Name: fmt.Sprintf("dict=%d", dictSize),
// 			Scores: tools.Scores{
// 				Exact:   avgUs,
// 				Partial: usPerEntry,
// 				Fuzzy:   float64(dictSize),
// 			},
// 			WeightedTotal: score,
// 		})
// 	}

// 	return nil
// }
