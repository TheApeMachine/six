package classification

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/vm/input"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"

	gc "github.com/smartystreets/goconvey/convey"
)

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
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewTextClassificationExperiment() *TextClassificationExperiment {
	experiment := &TextClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
			huggingface.DatasetWithLabelAppend(agNewsLabels),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithExpectation(0.30, 0.85),
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

func (experiment *TextClassificationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

// Prompts creates test prompts from the same dataset, stripping label
// suffixes via SUBSTRING holdout so the machine sees pure article text.
func (experiment *TextClassificationExperiment) Prompts() []string {
	return experiment.prompt
}

func (experiment *TextClassificationExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.MATCH
}

func (experiment *TextClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			results.TrueLabel = tools.OptionalLabel(label)
		}
	}

	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

/*
ComputePredictions delegates to the Evaluator for label string matching.
*/
func (experiment *TextClassificationExperiment) ComputePredictions() {
	experiment.evaluator.ComputePredictions(experiment.tableData)
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.30 (above random for 4 classes), Target = 0.85.
*/
func (experiment *TextClassificationExperiment) Outcome() (
	any, gc.Assertion, any,
) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TextClassificationExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *TextClassificationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TextClassificationExperiment) Artifacts() []tools.Artifact {
	numSamples := len(experiment.tableData)
	score := experiment.Score()

	experiment.ComputePredictions()
	metrics := experiment.evaluator.Metrics(experiment.tableData, numSamples)

	matrixFile := tools.Slugify(experiment.Name()) + "_scores"

	proseTemplate := `\subsection{Text Classification}
\label{sec:text_classification}

\paragraph{Task Description.}
The text classification experiment evaluates zero-shot topical categorisation
on the AG News dataset (\texttt{sh0416/ag\_news}).  Articles from four
categories---World, Sports, Business, and Science/Technology---are ingested
with their label appended.  At test time the label suffix is stripped via
substring holdout; the system must surface the correct category word through
value co-occurrence in its generated output.

\paragraph{Results.}
Table~\ref{tab:text_classification_metrics} summarises the classification
metrics across $N = {{.N}}$ test samples.
The confusion matrix is shown in Figure~\ref{fig:text_classification_confusion}.

{{if gt .Accuracy 0.7 -}}
\paragraph{Assessment.}
The substrate achieved strong topical separation, correctly routing the
majority of article value patterns to their ground-truth category attractors.
{{- else if gt .Accuracy 0.4 -}}
\paragraph{Assessment.}
The substrate demonstrated moderate classification capability.
Some categories are reliably separated while others exhibit value overlap,
suggesting attractor boundaries between topically adjacent classes could
benefit from a larger ingestion corpus.
{{- else -}}
\paragraph{Assessment.}
Classification accuracy was low.  With only $N = {{.N}}$ samples the
substrate may not have built sufficient attractor density to separate all
four AG News categories reliably.  Scaling the ingestion volume is expected
to improve per-class disambiguation.
{{- end}}

\begin{table}[htbp]
  \centering
  \caption{Text Classification --- summary metrics.}
  \label{tab:text_classification_metrics}
  \begin{tabular}{ll}
    \toprule
    \textbf{Metric} & \textbf{Value} \\
    \midrule
    Overall Accuracy  & {{.AccuracyPct}} \\
    Balanced Accuracy & {{.BalancedAccPct}} \\
    Macro-F1          & {{.MacroF1 | f3}} \\
    Mean Resonance    & {{.Score | f4}} \\
    Sample Size       & $N = {{.N}}$ \\
    \bottomrule
  \end{tabular}
\end{table}
`

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
				Template: proseTemplate,
				Data: map[string]any{
					"N":              numSamples,
					"Score":          score,
					"Accuracy":       metrics.Accuracy,
					"AccuracyPct":    fmt.Sprintf("%.1f\\%%", metrics.Accuracy*100),
					"BalancedAccPct": fmt.Sprintf("%.1f\\%%", metrics.BalancedAcc*100),
					"MacroF1":        metrics.MacroF1,
				},
			},
		},
	}
}
