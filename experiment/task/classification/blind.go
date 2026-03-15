package classification

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/vm/input"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
)

/*
BlindClassificationExperiment tests the ability of the system to classify
news articles into topical categories, using a dataset of news articles.
we are testing the ability of the system to classify articles into categories
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
}

// ag_news label indices → human readable names
var newsLabels = []string{"world", "sports", "business", "sci_tech"}

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
	}

	return experiment
}

func (experiment *BlindClassificationExperiment) ClassLabels() []string { return newsLabels }
func (experiment *BlindClassificationExperiment) Name() string          { return "Blind Text Classification" }
func (experiment *BlindClassificationExperiment) Section() string       { return "blind classification" }
func (experiment *BlindClassificationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}
func (experiment *BlindClassificationExperiment) Prompts() []string { return experiment.prompt }
func (experiment *BlindClassificationExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.MATCH
}

func (experiment *BlindClassificationExperiment) AddResult(results tools.ExperimentalData) {
	if dataset, ok := experiment.dataset.(*huggingface.Dataset); ok {
		if label, ok := dataset.LabelForSample(uint32(results.Idx)); ok {
			results.TrueLabel = tools.OptionalLabel(label)
		}
	}

	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome determines what we consider to be a minimal acceptable result.
While text classification is considered achievable, this variation on
that task is a little more up in the air, so we set a slightly higher
than random choice as an acceptable result.
*/
func (experiment *BlindClassificationExperiment) Outcome() (
	any, gc.Assertion, any,
) {
	return tools.Outcome(
		experiment.Score(),
		len(experiment.ClassLabels()),
		tools.ABOVERANDOM,
	)
}

func (experiment *BlindClassificationExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, data := range experiment.tableData {
		total += data.Scores.Exact
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *BlindClassificationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BlindClassificationExperiment) Artifacts() []tools.Artifact {
	n := len(experiment.tableData)
	score := experiment.Score()

	// Compute accuracy, balanced accuracy, macro-F1 from the confusion matrix.
	labels := agNewsLabels
	nc := len(labels)
	matrix := make([][]int, nc)
	for i := range matrix {
		matrix[i] = make([]int, nc)
	}
	for _, row := range experiment.tableData {
		if row.TrueLabel == nil || row.PredLabel == nil {
			continue
		}
		t, p := *row.TrueLabel, *row.PredLabel
		if t >= 0 && t < nc && p >= 0 && p < nc {
			matrix[t][p]++
		}
	}

	total, correct := 0, 0
	recallSum := 0.0
	f1Sum := 0.0
	validClasses := 0

	for c := range nc {
		rowSum := 0
		for j := 0; j < nc; j++ {
			rowSum += matrix[c][j]
			total += matrix[c][j]
			if c == j {
				correct += matrix[c][j]
			}
		}
		if rowSum > 0 {
			recallSum += float64(matrix[c][c]) / float64(rowSum)
		}
		tp := matrix[c][c]
		fp, fn := 0, 0
		for i := range nc {
			if i != c {
				fp += matrix[i][c]
				fn += matrix[c][i]
			}
		}
		prec, rec := 0.0, 0.0
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		if prec+rec > 0 {
			f1Sum += 2 * prec * rec / (prec + rec)
			validClasses++
		}
	}

	// Accuracy denominator: all N samples, not just predicted ones.
	// Unpredicted samples count as incorrect.
	accuracy := 0.0
	if n > 0 {
		accuracy = float64(correct) / float64(n)
	}
	balancedAcc := 0.0
	if nc > 0 {
		balancedAcc = recallSum / float64(nc)
	}
	macroF1 := 0.0
	if validClasses > 0 {
		macroF1 = f1Sum / float64(validClasses)
	}

	// Summary metrics table data.
	tableRows := [][]string{
		{"Metric", "Value"},
		{"Overall Accuracy", fmt.Sprintf("%.1f%%", accuracy*100)},
		{"Balanced Accuracy", fmt.Sprintf("%.1f%%", balancedAcc*100)},
		{"Macro-F1", fmt.Sprintf("%.3f", macroF1)},
		{"Mean Resonance", fmt.Sprintf("%.4f", score)},
		{"Predicted", fmt.Sprintf("%d / %d", total, n)},
		{"Sample Size (N)", fmt.Sprintf("%d", n)},
	}

	matrixFile := tools.Slugify(experiment.Name()) + "_scores"

	proseTemplate := `\subsection{Text Classification}
\label{sec:text_classification}

\paragraph{Task Description.}
The text classification experiment evaluates zero-shot topical categorisation
on the AG News dataset (\texttt{sh0416/ag\_news}).  Articles from four
categories---World, Sports, Business, and Science/Technology---are ingested
with their label appended.  At test time the label suffix is stripped via
substring holdout; the system must surface the correct category word through
chord co-occurrence in its generated output.

\paragraph{Results.}
Table~\ref{tab:text_classification_metrics} summarises the classification
metrics across $N = {{.N}}$ test samples.
The confusion matrix is shown in Figure~\ref{fig:text_classification_confusion}.

{{if gt .Accuracy 0.7 -}}
\paragraph{Assessment.}
The substrate achieved strong topical separation, correctly routing the
majority of article chord patterns to their ground-truth category attractors.
{{- else if gt .Accuracy 0.4 -}}
\paragraph{Assessment.}
The substrate demonstrated moderate classification capability.
Some categories are reliably separated while others exhibit chord overlap,
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
			FileName: tools.Slugify(experiment.Name()) + "_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"N":              n,
					"Score":          score,
					"Accuracy":       accuracy,
					"AccuracyPct":    fmt.Sprintf("%.1f\\%%", accuracy*100),
					"BalancedAccPct": fmt.Sprintf("%.1f\\%%", balancedAcc*100),
					"MacroF1":        macroF1,
					"TableRows":      tableRows,
				},
			},
		},
	}
}
