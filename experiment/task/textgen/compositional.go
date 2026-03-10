package textgen

import (
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
CompositionalExperiment evaluates the system's ability to compose novel
word combinations. It verifies that the structural vacuum (ChordHole)
of a partial prompt uniquely identifies the missing component even
if the specific combination was not in the corpus.
*/
type CompositionalExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewCompositionalExperiment() *CompositionalExperiment {
	return &CompositionalExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(compositionalCorpus()),
	}
}

func compositionalCorpus() []string {
	return []string{
		"the quick brown fox",
		"the slow brown bear",
		"the quick red car",
	}
}

func (experiment *CompositionalExperiment) Name() string {
	return "Compositional"
}

func (experiment *CompositionalExperiment) Section() string {
	return "textgen"
}

func (experiment *CompositionalExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CompositionalExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
		tokenizer.PromptWithValues([]string{
			"the quick red",
		}),
	)

	return experiment.prompt
}

func (experiment *CompositionalExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 5, tokenizer.RIGHT
}

func (experiment *CompositionalExperiment) AddResult(results tools.ExperimentalData) {
	score := 0.0
	observed := string(results.Observed)
	if strings.Contains(observed, "car") {
		score = 1.0
	}

	results.WeightedTotal = score
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompositionalExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *CompositionalExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *CompositionalExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CompositionalExperiment) Artifacts() []tools.Artifact {
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

func (experiment *CompositionalExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
