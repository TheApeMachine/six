package experiment

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestEvaluatorMetricsIgnoresUnresolvedPredictions(t *testing.T) {
	Convey("Classification metrics count unresolved rows as misses rather than predictions", t, func() {
		evaluator := NewEvaluator(EvalWithLabels([]string{"world", "sports"}))

		rows := []ExperimentalData{
			{
				TrueLabel:         OptionalLabel(0),
				PredLabel:         OptionalLabel(0),
				ExecutionSettled:  true,
				ReasoningResolved: true,
			},
			{
				TrueLabel:         OptionalLabel(1),
				PredLabel:         OptionalLabel(0),
				ExecutionSettled:  true,
				ReasoningResolved: false,
			},
		}

		metrics := evaluator.Metrics(rows, len(rows))

		So(metrics.Accuracy, ShouldAlmostEqual, 0.5, 1e-12)
		So(metrics.BalancedAcc, ShouldAlmostEqual, 0.5, 1e-12)
		So(metrics.MacroF1, ShouldAlmostEqual, 0.5, 1e-12)
	})
}

func TestEvalWithExpectationUsesLegacyDynamicBaseline(t *testing.T) {
	Convey("EvalWithExpectation rewrites the legacy magic baselines and keeps baseline gating", t, func() {
		evaluator := NewEvaluator(
			EvalWithExpectation(0.05, 0.50),
		)

		expectation := evaluator.Expectation()

		So(expectation.LegacyDynamicBaseline, ShouldBeTrue)
		So(expectation.Gate, ShouldEqual, ExpectationGateBaseline)
		So(expectation.Baseline, ShouldNotEqual, 0.05)
		So(expectation.Target, ShouldEqual, 0.50)

		score, assertion, threshold := evaluator.Outcome(0.25)
		So(score, ShouldEqual, 0.25)
		So(threshold, ShouldEqual, expectation.Baseline)
		So(assertion(0.25, threshold), ShouldEqual, "")
		// Below threshold must fail; avoid score 0.0 because a zero dynamic baseline makes 0 >= 0 a pass.
		So(assertion(-1.0, threshold), ShouldNotEqual, "")
	})
}

func TestEvalWithFixedExpectationPreservesThresholds(t *testing.T) {
	Convey("EvalWithFixedExpectation keeps the provided thresholds exactly", t, func() {
		evaluator := NewEvaluator(
			EvalWithFixedExpectation(0.30, 0.85),
		)

		expectation := evaluator.Expectation()

		So(expectation.LegacyDynamicBaseline, ShouldBeFalse)
		So(expectation.Gate, ShouldEqual, ExpectationGateBaseline)
		So(expectation.Baseline, ShouldEqual, 0.30)
		So(expectation.Target, ShouldEqual, 0.85)
	})
}

func TestEvalAssertTargetUsesTargetThreshold(t *testing.T) {
	Convey("EvalAssertTarget makes Outcome gate on the target threshold", t, func() {
		evaluator := NewEvaluator(
			EvalWithFixedExpectation(0.95, 1.0),
			EvalAssertTarget(),
		)

		expectation := evaluator.Expectation()
		So(expectation.Gate, ShouldEqual, ExpectationGateTarget)

		score, assertion, threshold := evaluator.Outcome(1.0)
		So(score, ShouldEqual, 1.0)
		So(threshold, ShouldEqual, 1.0)
		So(assertion(1.0, threshold), ShouldEqual, "")
		So(assertion(0.99, threshold), ShouldNotEqual, "")
	})
}

func TestEvalOutcomeEdgeCases(t *testing.T) {
	Convey("Outcome at score 0.0 fails baseline gate when threshold is positive", t, func() {
		evaluator := NewEvaluator(
			EvalWithFixedExpectation(0.10, 0.90),
		)

		expectation := evaluator.Expectation()
		So(expectation.Gate, ShouldEqual, ExpectationGateBaseline)

		score, assertion, threshold := evaluator.Outcome(0.0)
		So(score, ShouldEqual, 0.0)
		So(threshold, ShouldEqual, 0.10)
		So(assertion(0.0, threshold), ShouldNotEqual, "")
	})

	Convey("Outcome allows scores above 1.0 when asserting against target", t, func() {
		evaluator := NewEvaluator(
			EvalWithFixedExpectation(0.0, 1.0),
			EvalAssertTarget(),
		)

		score, assertion, threshold := evaluator.Outcome(1.25)
		So(score, ShouldEqual, 1.25)
		So(threshold, ShouldEqual, 1.0)
		So(assertion(1.25, threshold), ShouldEqual, "")
		So(assertion(0.99, threshold), ShouldNotEqual, "")
	})

	Convey("Outcome does not reject baseline greater than target; threshold follows gate", t, func() {
		evaluator := NewEvaluator(
			EvalWithFixedExpectation(0.95, 0.50),
		)

		score, assertion, threshold := evaluator.Outcome(0.96)
		So(score, ShouldEqual, 0.96)
		So(threshold, ShouldEqual, 0.95)
		So(assertion(0.96, threshold), ShouldEqual, "")
		So(assertion(0.94, threshold), ShouldNotEqual, "")

		evaluatorTarget := NewEvaluator(
			EvalWithFixedExpectation(0.95, 0.50),
			EvalAssertTarget(),
		)

		_, assertionT, thresholdT := evaluatorTarget.Outcome(0.60)
		So(thresholdT, ShouldEqual, 0.50)
		So(assertionT(0.60, thresholdT), ShouldEqual, "")
		So(assertionT(0.40, thresholdT), ShouldNotEqual, "")
	})
}
