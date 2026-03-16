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

/*
BlindClassificationExperiment tests the ability of the system to classify
news articles into topical categories, using a dataset of news articles.
We are testing the ability of the system to classify articles into categories
without having ever seen the explicit labels.
The intuition is that if we give the system enough news articles, and
ask it to assign each article to one of N categories, there is a chance
that it would be able to pick up on the "domain knowledge" of each
category, and be able to classify articles into categories it has never
seen before.
*/
type BlindClassificationExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewBlindClassificationExperiment() *BlindClassificationExperiment {
	experiment := &BlindClassificationExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("sh0416/ag_news"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithSplit("test"),
			huggingface.DatasetWithTextColumns("title", "description"),
			huggingface.DatasetWithLabelColumn("label"),
		),
		evaluator: tools.NewEvaluator(
			tools.EvalWithLabels(agNewsLabels),
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}

	return experiment
}

func (experiment *BlindClassificationExperiment) ClassLabels() []string {
	return agNewsLabels
}

func (experiment *BlindClassificationExperiment) Name() string {
	return "Blind Text Classification"
}

func (experiment *BlindClassificationExperiment) Section() string {
	return "blind classification"
}

func (experiment *BlindClassificationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BlindClassificationExperiment) Prompts() []string {
	return experiment.prompt
}

func (experiment *BlindClassificationExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.MATCH
}

func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {
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
func (experiment *BlindClassificationExperiment) ComputePredictions() {
	experiment.evaluator.ComputePredictions(experiment.tableData)
}

/*
Outcome delegates to the Evaluator which holds the real expectation
thresholds. Baseline = 0.05 (barely above noise for blind task),
Target = 0.50 (strong unsupervised clustering).
*/
func (experiment *BlindClassificationExperiment) Outcome() (
	any, gc.Assertion, any,
) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *BlindClassificationExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *BlindClassificationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BlindClassificationExperiment) Artifacts() []tools.Artifact {
	numSamples := len(experiment.tableData)
	score := experiment.Score()

	experiment.ComputePredictions()
	metrics := experiment.evaluator.Metrics(experiment.tableData, numSamples)

	matrixFile := tools.Slugify(experiment.Name()) + "_scores"

	proseTemplate := `\subsection{Blind Classification}
\label{sec:blind_classification}

\paragraph{Task Description.}
The blind classification experiment evaluates zero-shot topical categorisation
on the AG News dataset (\texttt{sh0416/ag\_news}) \emph{without} label
supervision.  Unlike the standard text classification variant, category
labels are never appended during ingestion---the system must discover
topical structure purely from value co-occurrence patterns in the article
text.  At test time the system must surface the correct category word
through associative recall alone.

\paragraph{Results.}
Table~\ref{tab:blind_classification_metrics} summarises the classification
metrics across $N = {{.N}}$ test samples.
The confusion matrix is shown in Figure~\ref{fig:blind_classification_confusion}.

{{if gt .Accuracy 0.7 -}}
\paragraph{Assessment.}
The substrate achieved strong topical separation even without explicit
label supervision, indicating robust attractor formation from article
content alone.
{{- else if gt .Accuracy 0.4 -}}
\paragraph{Assessment.}
The substrate demonstrated moderate blind classification capability.
Some categories are separable through content co-occurrence alone, while
others require label reinforcement for reliable disambiguation.
{{- else -}}
\paragraph{Assessment.}
Blind classification accuracy was low.  Without label supervision the
substrate relies entirely on structural similarity between article content
and category words.  Scaling ingestion volume or enriching the corpus with
category-adjacent vocabulary may improve attractor formation.
{{- end}}

\begin{table}[htbp]
  \centering
  \caption{Blind Classification --- summary metrics.}
  \label{tab:blind_classification_metrics}
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
			FileName: tools.Slugify(experiment.Name()) + "_section.tex",
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
