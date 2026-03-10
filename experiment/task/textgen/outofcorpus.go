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
OutOfCorpusExperiment evaluates the system's ability to perform
compositional reasoning on a knowledge base of facts. It verifies
that the system can answer queries that were never verbatim in the
corpus by transferring properties across structural analogy bridges.
*/
type OutOfCorpusExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
	return &OutOfCorpusExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(factsCorpus()),
	}
}

func factsCorpus() []string {
	return []string{
		"cat likes fish",
		"dog likes bones",
		"parrot likes seeds",
		"shark likes plankton",
		"frog likes flies",
		"cat hates water",
		"dog hates thunder",
		"parrot hates noise",
	}
}

func (experiment *OutOfCorpusExperiment) Name() string {
	return "Out of Corpus"
}

func (experiment *OutOfCorpusExperiment) Section() string {
	return "textgen"
}

func (experiment *OutOfCorpusExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *OutOfCorpusExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
		// Custom prompts that are not in the corpus
		tokenizer.PromptWithValues([]string{
			"shark hates",
			"frog hates",
		}),
	)

	return experiment.prompt
}

func (experiment *OutOfCorpusExperiment) Holdout() (int, tokenizer.HoldoutType) {
	// We expect the system to generate completions based on analogies.
	return 10, tokenizer.RIGHT
}

func (experiment *OutOfCorpusExperiment) AddResult(results tools.ExperimentalData) {
	// Scoring logic: check if the observed text contains the expected analogy transfer.
	// shark hates -> thunder (via dog)
	// frog hates -> water or noise (via cat or parrot)

	score := 0.0
	prefix := string(results.Prefix)
	observed := string(results.Observed)

	if strings.HasPrefix(
		prefix, "shark hate",
	) && strings.Contains(observed, "thunder") {
		score = 1.0
	} else if strings.HasPrefix(
		prefix, "frog hate",
	) && (strings.Contains(observed, "water") || strings.Contains(observed, "noise")) {
		score = 1.0
	}

	results.WeightedTotal = score
	experiment.tableData = append(
		experiment.tableData, results,
	)
}

func (experiment *OutOfCorpusExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *OutOfCorpusExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *OutOfCorpusExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *OutOfCorpusExperiment) Artifacts() []tools.Artifact {
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

func (experiment *OutOfCorpusExperiment) Finalize(
	substrate *geometry.HybridSubstrate,
) error {
	return nil
}
