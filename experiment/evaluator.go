package experiment

import (
	"crypto/rand"
	"math"
	"sync"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

/*
Expectation defines the scoring thresholds for an experiment.
Baseline is the regression floor — if the score drops below it,
the test fails. Target is the aspirational goal. Gate selects which
threshold Outcome asserts against. LegacyDynamicBaseline reports whether
the baseline was rewritten by the legacy magic-number helper.
*/
type Expectation struct {
	Baseline              float64
	Target                float64
	Gate                  ExpectationGate
	LegacyDynamicBaseline bool
}

type ExpectationGate uint8

const (
	ExpectationGateBaseline ExpectationGate = iota
	ExpectationGateTarget
)

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

func (evaluator *Evaluator) Expectation() Expectation {
	if evaluator == nil {
		return Expectation{}
	}

	return evaluator.expectation
}

/*
Outcome produces the GoConvey assertion triple using the configured
Expectation gate. This replaces the pattern of every experiment
hardcoding its own thresholding logic.
*/
func (evaluator *Evaluator) Outcome(score float64) (any, gc.Assertion, any) {
	return score, gc.ShouldBeGreaterThanOrEqualTo, evaluator.expectation.threshold()
}

/*
OutcomeForPromptConvey is the Convey-oriented entry for per-prompt assertions;
it forwards to OutcomeForTableRow so pipeline tests share one implementation.
*/
func (evaluator *Evaluator) OutcomeForPromptConvey(table []ExperimentalData, idx int) (any, gc.Assertion, any) {
	return evaluator.OutcomeForTableRow(table, idx)
}

/*
OutcomeForTableRow is the per-prompt counterpart to Outcome: it uses the same
threshold as Outcome() but scores only the row at idx.

Classification experiments (non-empty Evaluator labels): the scalar is 1 when
the predicted class matches the gold label after ComputePredictions on that
row, else 0 — comparable to the same baseline gate as the aggregate, as a
per-sample pass/fail signal.

Holdout experiments: the scalar is Scorer.GateScore (e.g. WeightedTotal or
Scores.Exact), matching the aggregate’s scorer scale.
*/
func (evaluator *Evaluator) OutcomeForTableRow(table []ExperimentalData, idx int) (any, gc.Assertion, any) {
	if evaluator == nil || idx < 0 || idx >= len(table) {
		return nil, gc.ShouldBeNil, nil
	}

	thresh := evaluator.expectation.threshold()

	if evaluator.scorer == nil {
		return nil, gc.ShouldBeNil, nil
	}

	return evaluator.scorer.GateScore(table[idx]), gc.ShouldBeGreaterThanOrEqualTo, thresh
}

func (expectation Expectation) threshold() float64 {
	if expectation.Gate == ExpectationGateTarget {
		return expectation.Target
	}

	return expectation.Baseline
}

/*
Metrics computes accuracy, balanced accuracy, and macro-F1 from the
confusion matrix built over data. numSamples is the total experiment
sample count (used as the accuracy denominator so that unpredicted
samples count as incorrect).

Rows with ExecutionSettled and ReasoningResolved == false are omitted from
the matrix (they count as misses for accuracy via numSamples, not as
predictions). Macro-F1 averages over every label class, using F1=0 when a
class has no support in the matrix.
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

		if row.ExecutionSettled && !row.ReasoningResolved {
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

		f1 := 0.0

		if precision+recall > 0 {
			f1 = 2 * precision * recall / (precision + recall)
		}

		f1Sum += f1
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
	if numClasses > 0 {
		macroF1 = f1Sum / float64(numClasses)
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

var (
	cachedBaseline     float64
	cachedBaselineOnce sync.Once
)

func calculateRandomBaseline() float64 {
	cachedBaselineOnce.Do(func() {
		const samples = 100
		scorer := &HoldoutScorer{}
		var scores []float64
		var sum float64

		for range samples {
			obs := make([]byte, core.Cfg.Value.Bytes)
			hold := make([]byte, core.Cfg.Value.Bytes)
			_, _ = rand.Read(obs)
			_, _ = rand.Read(hold)

			data := ExperimentalData{
				Generation: obs,
				Holdout:    hold,
			}

			scorer.Enrich(&data)
			sum += data.WeightedTotal
			scores = append(scores, data.WeightedTotal)
		}

		mean := sum / float64(samples)
		var varianceSum float64

		for _, s := range scores {
			diff := s - mean
			varianceSum += diff * diff
		}

		variance := varianceSum / float64(samples)
		stddev := math.Sqrt(variance)

		// Expected random score + 3 standard deviations
		cachedBaseline = mean + 3*stddev
	})

	return cachedBaseline
}

/*
legacyExpectationBaselines are the discrete baseline floors historically passed to
EvalWithExpectation across early task constructors (0.05 was the common regression
floor; 0.03 / 0.10 / 0.20 / 0.30 appeared as task-specific defaults before
calculateRandomBaseline existed). Any incoming baseline equal to one of these
still opts into the legacy dynamic rewrite for backward compatibility with older
call sites that relied on magic numbers rather than measured random performance.
*/
var legacyExpectationBaselines = []float64{0.05, 0.03, 0.10, 0.20, 0.30}

func isLegacyBaseline(value float64) bool {
	for _, legacy := range legacyExpectationBaselines {
		if value == legacy {
			return true
		}
	}

	return false
}

/*
EvalWithExpectation sets the baseline and target thresholds.
Baseline is the regression floor. Target is the aspirational goal.
*/
func EvalWithExpectation(baseline, target float64) evalOpts {
	return func(evaluator *Evaluator) {
		dynamic := false

		// Use dynamic baseline if the provided baseline looks like the old hardcoded magic logic
		if isLegacyBaseline(baseline) {
			baseline = calculateRandomBaseline()
			dynamic = true
		}

		evaluator.expectation = Expectation{
			Baseline:              baseline,
			Target:                target,
			Gate:                  ExpectationGateBaseline,
			LegacyDynamicBaseline: dynamic,
		}
	}
}

/*
EvalWithFixedExpectation preserves the provided baseline and target exactly.
Use this when the threshold has been intentionally chosen and should not be
rewritten by the legacy dynamic-baseline helper.
*/
func EvalWithFixedExpectation(baseline, target float64) evalOpts {
	return func(evaluator *Evaluator) {
		evaluator.expectation = Expectation{
			Baseline: baseline,
			Target:   target,
			Gate:     ExpectationGateBaseline,
		}
	}
}

/*
EvalAssertTarget makes Outcome assert against the configured target rather than
the regression baseline. Pair this with EvalWithFixedExpectation when an
experiment is mature enough to require its target score in tests.
*/
func EvalAssertTarget() evalOpts {
	return func(evaluator *Evaluator) {
		evaluator.expectation.Gate = ExpectationGateTarget
	}
}

