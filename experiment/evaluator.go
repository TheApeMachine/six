package experiment

import (
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
Expectation defines the scoring thresholds for an experiment.
Baseline is the regression floor — if the score drops below it,
the test fails. Target is the aspirational goal (unused by assertions
but reported in artifacts for context).
*/
type Expectation struct {
	Baseline float64
	Target   float64
}

/*
Evaluator centralizes scoring, prediction, and outcome assertion
logic so experiment objects remain thin dataset-plus-template shells.

It holds a Scorer for per-result enrichment and aggregate scoring,
optional class labels for classification experiments, and an
Expectation for producing meaningful test assertions.
*/
type Evaluator struct {
	scorer      Scorer
	labels      []string
	expectation Expectation
}

type evalOpts func(*Evaluator)

/*
NewEvaluator instantiates a new Evaluator. If no Scorer is provided,
HoldoutScorer is used as the default.
*/
func NewEvaluator(opts ...evalOpts) *Evaluator {
	evaluator := &Evaluator{
		scorer: &HoldoutScorer{},
	}

	for _, opt := range opts {
		opt(evaluator)
	}

	return evaluator
}

/*
Enrich delegates per-result enrichment to the configured Scorer.
*/
func (evaluator *Evaluator) Enrich(data *ExperimentalData) {
	evaluator.scorer.Enrich(data)
}

/*
MeanScore delegates aggregate scoring to the configured Scorer.
*/
func (evaluator *Evaluator) MeanScore(data []ExperimentalData) float64 {
	return evaluator.scorer.Aggregate(data)
}

/*
Outcome produces the GoConvey assertion triple using the configured
Expectation baseline. This replaces the pattern of every experiment
hardcoding its own (often meaningless) threshold.
*/
func (evaluator *Evaluator) Outcome(score float64) (any, gc.Assertion, any) {
	return score, gc.ShouldBeGreaterThanOrEqualTo, evaluator.expectation.Baseline
}

/*
ComputePredictions assigns PredLabel by checking which label string
co-occurs in the machine's generated output.

Scoring:
  - Exactly one label found → confident prediction.
  - Multiple labels found  → ambiguous, discard (PredLabel = nil).
  - No labels found        → no prediction (PredLabel = nil).
*/
func (evaluator *Evaluator) ComputePredictions(data []ExperimentalData) {
	if len(data) == 0 || len(evaluator.labels) == 0 {
		return
	}

	numClasses := len(evaluator.labels)

	for idx := range data {
		data[idx].PredLabel = nil

		generated := string(data[idx].Observed)

		if len(generated) == 0 {
			continue
		}

		var found []int

		for classIdx := range numClasses {
			if strings.Contains(generated, evaluator.labels[classIdx]) {
				found = append(found, classIdx)
			}
		}

		if len(found) == 1 {
			data[idx].PredLabel = OptionalLabel(found[0])
		}
	}
}

/*
Metrics computes accuracy, balanced accuracy, and macro-F1 from the
confusion matrix built over data. numSamples is the total experiment
sample count (used as the accuracy denominator so that unpredicted
samples count as incorrect).
*/
func (evaluator *Evaluator) Metrics(data []ExperimentalData, numSamples int) ClassificationMetrics {
	numClasses := len(evaluator.labels)

	matrix := make([][]int, numClasses)
	for row := range matrix {
		matrix[row] = make([]int, numClasses)
	}

	for _, row := range data {
		if row.TrueLabel == nil || row.PredLabel == nil {
			continue
		}

		trueIdx, predIdx := *row.TrueLabel, *row.PredLabel

		if trueIdx >= 0 && trueIdx < numClasses && predIdx >= 0 && predIdx < numClasses {
			matrix[trueIdx][predIdx]++
		}
	}

	total, correct := 0, 0
	recallSum := 0.0
	f1Sum := 0.0
	validClasses := 0

	for classIdx := range numClasses {
		rowSum := 0

		for predIdx := range numClasses {
			rowSum += matrix[classIdx][predIdx]
			total += matrix[classIdx][predIdx]

			if classIdx == predIdx {
				correct += matrix[classIdx][predIdx]
			}
		}

		if rowSum > 0 {
			recallSum += float64(matrix[classIdx][classIdx]) / float64(rowSum)
		}

		truePositive := matrix[classIdx][classIdx]
		falsePositive, falseNegative := 0, 0

		for otherIdx := range numClasses {
			if otherIdx != classIdx {
				falsePositive += matrix[otherIdx][classIdx]
				falseNegative += matrix[classIdx][otherIdx]
			}
		}

		precision, recall := 0.0, 0.0

		if truePositive+falsePositive > 0 {
			precision = float64(truePositive) / float64(truePositive+falsePositive)
		}

		if truePositive+falseNegative > 0 {
			recall = float64(truePositive) / float64(truePositive+falseNegative)
		}

		if precision+recall > 0 {
			f1Sum += 2 * precision * recall / (precision + recall)
			validClasses++
		}
	}

	accuracy := 0.0
	if numSamples > 0 {
		accuracy = float64(correct) / float64(numSamples)
	}

	balancedAcc := 0.0
	if numClasses > 0 {
		balancedAcc = recallSum / float64(numClasses)
	}

	macroF1 := 0.0
	if validClasses > 0 {
		macroF1 = f1Sum / float64(validClasses)
	}

	return ClassificationMetrics{
		Matrix:      matrix,
		Accuracy:    accuracy,
		BalancedAcc: balancedAcc,
		MacroF1:     macroF1,
		Total:       total,
		Correct:     correct,
	}
}

/*
ClassificationMetrics holds all derived metrics from a confusion matrix.
*/
type ClassificationMetrics struct {
	Matrix      [][]int
	Accuracy    float64
	BalancedAcc float64
	MacroF1     float64
	Total       int
	Correct     int
	MeanScore   float64
}

/*
EvalWithScorer configures a custom Scorer strategy.
*/
func EvalWithScorer(scorer Scorer) evalOpts {
	return func(evaluator *Evaluator) {
		evaluator.scorer = scorer
	}
}

/*
EvalWithLabels configures label-based evaluation (classification).
*/
func EvalWithLabels(labels []string) evalOpts {
	return func(evaluator *Evaluator) {
		evaluator.labels = labels
	}
}

/*
EvalWithExpectation sets the baseline and target thresholds.
Baseline is the regression floor. Target is the aspirational goal.
*/
func EvalWithExpectation(baseline, target float64) evalOpts {
	return func(evaluator *Evaluator) {
		evaluator.expectation = Expectation{
			Baseline: baseline,
			Target:   target,
		}
	}
}
