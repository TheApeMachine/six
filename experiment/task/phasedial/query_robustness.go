package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
)

/*
QueryRobustnessExperiment evaluates the topological resilience of the PhaseDial
to corrupted inputs. It demonstrates that the system can resolve correct
readouts from queries with 30% character dropout by scanning the phase torus.
*/
type QueryRobustnessExperiment struct {
	tableData         []tools.ExperimentalData
	robustnessResults []robustnessEntry
	dataset           provider.Dataset
	prompt            *process.Prompt
}

func NewQueryRobustnessExperiment() *QueryRobustnessExperiment {
	return &QueryRobustnessExperiment{
		tableData:         []tools.ExperimentalData{},
		robustnessResults: []robustnessEntry{},
		dataset:           tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *QueryRobustnessExperiment) Name() string {
	return "Query Robustness"
}

func (experiment *QueryRobustnessExperiment) Section() string {
	return "phasedial"
}

func (experiment *QueryRobustnessExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *QueryRobustnessExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *QueryRobustnessExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *QueryRobustnessExperiment) AddResult(results tools.ExperimentalData) {
	// Custom scoring logic for robustness
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *QueryRobustnessExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *QueryRobustnessExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *QueryRobustnessExperiment) TableData() any {
	return experiment.tableData
}

type robustnessEntry struct {
	Query      string
	ScanSteps  int
	Step0Match string
	CorruptSim string
}

func (experiment *QueryRobustnessExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "query_robustness_summary.tex",
			Data:     experiment.robustnessResults,
			Title:    "Query Robustness Summary",
			Caption:  "Resilience of PhaseDial retrieval to character dropout.",
			Label:    "tab:query_robustness",
		},
	}
}

func (experiment *QueryRobustnessExperiment) RawOutput() bool { return false }

// func (experiment *QueryRobustnessExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
// 	rng := rand.New(rand.NewSource(7))

// 	// Clean query from substrate entry 0
// 	cleanChords := substrate.Entries[0].Readout
// 	cleanFP := substrate.Entries[0].Fingerprint

// 	// Corrupt 30% of chords by clearing random bits
// 	corruptedChords := make([]data.Chord, len(cleanChords))
// 	copy(corruptedChords, cleanChords)
// 	for i := range corruptedChords {
// 		if rng.Float32() < 0.3 {
// 			corruptedChords[i] = data.Chord{} // zero out the chord
// 		}
// 	}
// 	corruptedFP := geometry.NewPhaseDial().EncodeFromChords(corruptedChords)

// 	corruptedResults := substrate.GeodesicScan(corruptedFP, 72, 5.0)
// 	cleanResults := substrate.GeodesicScan(cleanFP, 72, 5.0)

// 	sim := corruptedFP.Similarity(cleanFP)

// 	experiment.robustnessResults = []robustnessEntry{
// 		{
// 			Query:      "Clean",
// 			ScanSteps:  len(cleanResults),
// 			Step0Match: fmt.Sprintf("entry-%d", cleanResults[0].BestIdx),
// 			CorruptSim: "1.0000",
// 		},
// 		{
// 			Query:      fmt.Sprintf("Corrupted (30%% dropout, %d chords)", len(corruptedChords)),
// 			ScanSteps:  len(corruptedResults),
// 			Step0Match: fmt.Sprintf("entry-%d", corruptedResults[0].BestIdx),
// 			CorruptSim: fmt.Sprintf("%.4f", sim),
// 		},
// 	}

// 	experiment.AddResult(tools.ExperimentalData{
// 		Name:          "Robustness",
// 		WeightedTotal: sim,
// 	})

// 	return nil
// }
