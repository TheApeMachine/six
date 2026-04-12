package classification

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
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
			huggingface.DatasetWithLabelAppend(agNewsLabels),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithFixedExpectation(0.30, 0.85),
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

func (experiment *TextClassificationExperiment) Dataset() data.Provider {
	return experiment.dataset
}

// Prompts builds one prompt per structured sample: article text without the
// classification suffix (GeneratePrompts.Text). Holdout keeps the exact gold
// label string on the scoring side; prompt-time observation must recover that
// label from staged corpus evidence rather than from byte overlap shortcuts.
func (experiment *TextClassificationExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	pp, ok := experiment.dataset.(data.PromptProvider)
	if !ok {
		return experiment.prompt
	}
	for p := range pp.GeneratePrompts() {
		if p.Text == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, p.Text)
		var ho []byte
		if p.HasLabel && p.Label != "" {
			ho = []byte(p.Label)
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
	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			if normalized, ok := normalizeClassificationLabelIndex(label, experiment.ClassLabels()); ok {
				results.TrueLabel = tools.OptionalLabel(normalized)
			}
		}
	}

	if results.PredLabel != nil && len(results.Classification) > 0 {
		experiment.evaluator.Enrich(&results)
	} else {
		results.Scores = tools.Scores{}
		results.WeightedTotal = 0
	}

	experiment.predictionsComputed = false
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextClassificationExperiment) ensurePredictions() {
	if experiment.predictionsComputed {
		return
	}

	for idx := range experiment.tableData {
		row := &experiment.tableData[idx]

		if row.PredLabel != nil && len(row.Classification) > 0 {
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

		if row.PredLabel != nil && len(row.Classification) > 0 {
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
	score := experiment.Score()
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
\end{table}`,
			projector.F3(metrics.MacroF1),
			fmt.Sprintf("%.1f\\%%", metrics.Accuracy*100),
			fmt.Sprintf("%.1f\\%%", metrics.BalancedAcc*100),
			projector.F4(score),
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
