package misc

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
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
	prompt    *tokenizer.Prompt
	manifold  [][]byte
	seen      map[string]struct{}
}

func NewProteinStructureExperiment() *ProteinStructureExperiment {
	experiment := &ProteinStructureExperiment{
		tableData: []tools.ExperimentalData{},
		manifold:  make([][]byte, 0),
		seen:      make(map[string]struct{}),
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("proteinea/secondary_structure_prediction"),
			huggingface.DatasetWithSamples(20),
			huggingface.DatasetWithTextColumns("input", "dssp3"),
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

func (experiment *ProteinStructureExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *ProteinStructureExperiment) Holdout() (int, tokenizer.HoldoutType) {
	// Hold out the last 50 bytes for structure prediction
	return 50, tokenizer.RIGHT
}

/*
AddResult records an experimental observation.
*/
func (experiment *ProteinStructureExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(results.Scores.Exact, results.Scores.Partial, results.Scores.Fuzzy)

	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome evaluates the overall result. Secondary structure prediction
from raw bytes with zero training is extremely challenging — a score
above 0.3 is already interesting (random baseline is ~0.33 for 3-class).
*/
func (experiment *ProteinStructureExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.3
}

func (experiment *ProteinStructureExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *ProteinStructureExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *ProteinStructureExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: tools.Slugify(experiment.Name()) + "_scores",
			Data:     experiment.tableData,
			Title:    experiment.Name() + " — Score Breakdown",
			Caption:  "Mean exact, partial, fuzzy, and weighted scores for " + experiment.Name() + ".",
			Label:    "fig:" + tools.Slugify(experiment.Name()) + "_scores",
		},
	}
}

func (experiment *ProteinStructureExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
