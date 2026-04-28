package classification

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

var _ tools.HoldoutProvider = (*TextClassificationExperiment)(nil)

// ag_news label indices → human readable names
var agNewsLabels = []string{"world", "sports", "business", "sci_tech"}

/*
TextClassificationExperiment tests the ability of the system to classify
news articles into topical categories, using a dataset of news articles.
The minimal honest version uses the included labels, which span 4 categories,
however it could be an additional test to see if the system can classify
articles into more granular categories, without having ever seen the
explicit labels.
The intuition is that if we give the system enough news articles, and
ask it to assign each article to one of N categories, there is a chance
that it would be able to pick up on the "domain knowledge" of each
category, and be able to classify articles into categories it has never
seen before.
*/
type TextClassificationExperiment struct {
	tableData           []tools.ExperimentalData
	dataset             data.Provider
	prompt              []string
	holdouts            [][]byte
	evaluator           *tools.Evaluator
	predictionsComputed bool
}

func NewTextClassificationExperiment() *TextClassificationExperiment {
	experiment := &TextClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
			// ag_news ships labels in [1, 4] (1=World, 2=Sports, 3=Business,
			// 4=Sci/Tech). Declare the origin so the streaming layer
			// normalizes to [0, 3] internally; the substrate then re-shifts
			// to 1-indexed when writing the LABELS property word so slot
			// value 0 stays reserved as the unlabeled sentinel.
			huggingface.DatasetWithLabelOrigin(1),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithFixedExpectation(0.00, 0.85),
		),
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

func (experiment *TextClassificationExperiment) PromptFirmware() core.FirmwareType {
	return core.CLASSIFY_READOUT
}

func (experiment *TextClassificationExperiment) Dataset() data.Provider {
	return experiment.dataset
}

// Prompts builds one prompt per structured sample: TaskPrompt is the article
// (no classification suffix in the prompt string). Holdout keeps the gold
// label bytes for scoring.
func (experiment *TextClassificationExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.dataset.Generate() {
		task := string(sample.TaskPrompt())
		if task == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, task)

		var ho []byte
		if len(sample.Label) > 0 {
			ho = sample.Label
		}

		experiment.holdouts = append(experiment.holdouts, ho)
	}

	return experiment.prompt
}

func (experiment *TextClassificationExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if len(results.Resolved) > 0 {
		// Vote among all resolved segments
		votes := make(map[uint64]int)
		bestLabel := uint64(0)
		bestVotes := 0

		for _, value := range results.Resolved {
			labelWord, err := value.Property(primitive.LABELS)
			if err == nil && labelWord > 0 {
				votes[labelWord]++
				if votes[labelWord] > bestVotes {
					bestVotes = votes[labelWord]
					bestLabel = labelWord
				}
			}
		}

		if bestLabel > 0 {
			if int(bestLabel-1) < len(experiment.ClassLabels()) {
				results.PredLabel = tools.OptionalLabel(int(bestLabel - 1))
			}
		}
	}

	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			if label >= 0 && label < len(experiment.ClassLabels()) {
				results.TrueLabel = new(label)
			}
		}
	}

	if results.PredLabel != nil {
		experiment.evaluator.Enrich(&results)
	} else {
		results.Scores = tools.Scores{}
		results.WeightedTotal = 0
	}

	experiment.predictionsComputed = false
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextClassificationExperiment) LabelForPrompt(idx int) []byte {
	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(idx)); ok {
			if label >= 0 && label < len(experiment.ClassLabels()) {
				return []byte(experiment.ClassLabels()[label])
			}
		}
	}

	return nil
}

func (experiment *TextClassificationExperiment) ensurePredictions() {
	if experiment.predictionsComputed {
		return
	}

	for idx := range experiment.tableData {
		row := &experiment.tableData[idx]

		if row.PredLabel != nil {
			experiment.evaluator.Enrich(row)
		} else {
			row.Scores = tools.Scores{}
			row.WeightedTotal = 0
		}
	}

	experiment.predictionsComputed = true
}

/*
ComputePredictions re-materializes per-row class hypotheses from generation
and beam continuations, matching ensurePredictions without the cached flag gate.
*/
func (experiment *TextClassificationExperiment) ComputePredictions() {
	for idx := range experiment.tableData {
		row := &experiment.tableData[idx]

		if row.PredLabel != nil {
			experiment.evaluator.Enrich(row)
		} else {
			row.Scores = tools.Scores{}
			row.WeightedTotal = 0
		}
	}

	experiment.predictionsComputed = true
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.30, Target = 0.85. The score is exact
macro-averaged F1 over predicted labels, not byte-overlap resonance.
*/
func (experiment *TextClassificationExperiment) Outcome() (
	any, Assertion, any,
) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TextClassificationExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *TextClassificationExperiment) Score() float64 {
	experiment.ensurePredictions()
	n := len(experiment.tableData)
	if n == 0 {
		return 0
	}

	metrics := experiment.evaluator.Metrics(experiment.tableData, n)

	return metrics.MacroF1
}

func (experiment *TextClassificationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TextClassificationExperiment) Artifacts() []tools.Artifact {
	numSamples := len(experiment.tableData)
	metrics := experiment.evaluator.Metrics(experiment.tableData, numSamples)

	matrixFile := tools.Slugify(experiment.Name()) + "_scores"

	section := tools.ExperimentSection{
		Title: "Text Classification",
		Label: "text_classification",
		TaskDescription: `The text classification experiment evaluates zero-shot topical categorisation
on the AG News dataset (\texttt{sh0416/ag\_news}).  Articles from four
categories---World, Sports, Business, and Science/Technology---are ingested
with their label appended.  At test time the label suffix is stripped via
substring holdout; the system must recover the correct category word from the
closest resident labelled article in the substrate.`,
		Results: fmt.Sprintf(`Table~\ref{tab:text_classification_metrics} summarises the classification
metrics across $N = %d$ test samples.
The confusion matrix is shown in Figure~\ref{fig:text_classification_confusion}.`,
			numSamples),
		Assessment: textClassificationAssessment(metrics.MacroF1, numSamples),
		Table: fmt.Sprintf(`\begin{table}[htbp]
  \centering
  \caption{Text Classification --- summary metrics.}
  \label{tab:text_classification_metrics}
  \begin{tabular}{ll}
    \toprule
    \textbf{Metric} & \textbf{Value} \\
    \midrule
    Macro-F1          & %s \\
    Overall Accuracy  & %s \\
    Balanced Accuracy & %s \\
    Exact Score       & %s \\
    Sample Size       & $N = %d$ \\
    \bottomrule
  \end{tabular}
  {\footnotesize Exact Score is the exact-label match rate, computed as correct predictions divided by $N$; for this single-label task it matches Overall Accuracy. Balanced Accuracy is the unweighted mean of per-class recalls computed from the confusion matrix.}
\end{table}`,
			projector.Pct(metrics.MacroF1),
			projector.Pct(metrics.Accuracy),
			projector.Pct(metrics.BalancedAcc),
			projector.Pct(metrics.Accuracy),
			numSamples),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactConfusionMatrix,
			FileName: matrixFile,
			Data:     experiment.tableData,
			Title:    experiment.Name() + " — Confusion Matrix",
			Caption:  "Confusion matrix showing predicted vs. true class assignments for " + experiment.Name() + ".",
			Label:    "fig:" + tools.Slugify(experiment.Name()) + "_confusion",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "text_classification_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func textClassificationAssessment(macroF1 float64, n int) string {
	switch {
	case macroF1 > 0.7:
		return `The substrate achieved strong topical separation, correctly routing the
majority of article value patterns to their ground-truth category attractors.`
	case macroF1 > 0.4:
		return `The substrate demonstrated moderate classification capability.
Some categories are reliably separated while others exhibit value overlap,
suggesting attractor boundaries between topically adjacent classes could
benefit from a larger ingestion corpus.`
	default:
		return fmt.Sprintf(`Classification accuracy was low.  With only $N = %d$ samples the
substrate may not have built sufficient attractor density to separate all
four AG News categories reliably.  Scaling the ingestion volume is expected
to improve per-class disambiguation.`, n)
	}
}
