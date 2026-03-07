package classification

import (
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
TextClassificationExperiment tests the ability of the system to classify
news articles into topical categories via topological resonance.

Training data includes labels appended to article text so the manifold
stores the article→label association. Prompts use SUBSTRING holdout to
strip labels, so the machine sees pure article text and must surface the
label through co-occurrence in its generated output.
*/
type TextClassificationExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

// ag_news label indices → human readable names
var agNewsLabels = []string{"world", "sports", "business", "sci_tech"}

// labelSuffixes are the exact strings appended by DatasetWithLabelAppend,
// used by SUBSTRING holdout to strip them from prompts.
var labelSuffixes = func() []string {
	out := make([]string, len(agNewsLabels))
	for i, l := range agNewsLabels {
		out[i] = " " + l
	}
	return out
}()

func NewTextClassificationExperiment() *TextClassificationExperiment {
	experiment := &TextClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(100),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
			huggingface.DatasetWithLabelAppend(agNewsLabels),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.5
			},
			Description: "The system is able to classify text into correct categories.",
		},
	}

	return experiment
}

func (experiment *TextClassificationExperiment) ClassLabels() []string {
	return agNewsLabels
}

func (experiment *TextClassificationExperiment) Name() string {
	return "Text Classification"
}

func (experiment *TextClassificationExperiment) Section() string {
	return "classification"
}

func (experiment *TextClassificationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

// Prompts creates test prompts from the same dataset, stripping label
// suffixes via SUBSTRING holdout so the machine sees pure article text.
func (experiment *TextClassificationExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithSubstringHoldout(labelSuffixes),
	)
	return experiment.prompt
}

func (experiment *TextClassificationExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.SUBSTRING
}

func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			results.TrueLabel = tools.OptionalLabel(label)
		}
	}

	byteScores := tools.ByteScores(results.Holdout, results.Observed)
	results.Scores = tools.Scores{
		Exact:   byteScores["exact"],
		Partial: byteScores["partial"],
		Fuzzy:   byteScores["fuzzy"],
	}
	results.WeightedTotal = tools.WeightedTotal(results.Scores.Exact, results.Scores.Partial, results.Scores.Fuzzy)

	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextClassificationExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *TextClassificationExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *TextClassificationExperiment) TableData() []tools.ExperimentalData {
	return experiment.tableData
}

/*
ComputePredictions assigns PredLabel by checking which label string
co-occurs in the machine's generated output.

Scoring:
  - Exactly one label found → confident prediction.
  - Multiple labels found  → ambiguous, discard (PredLabel = nil).
  - No labels found        → no prediction (PredLabel = nil).
*/
func (experiment *TextClassificationExperiment) ComputePredictions() {
	n := len(experiment.tableData)
	if n == 0 {
		return
	}

	numClasses := len(agNewsLabels)

	for i := range experiment.tableData {
		experiment.tableData[i].PredLabel = nil

		generated := string(experiment.tableData[i].Observed)
		if len(generated) == 0 {
			continue
		}

		var found []int
		for c := 0; c < numClasses; c++ {
			if strings.Contains(generated, agNewsLabels[c]) {
				found = append(found, c)
			}
		}

		if len(found) == 1 {
			experiment.tableData[i].PredLabel = tools.OptionalLabel(found[0])
		}
	}
}
