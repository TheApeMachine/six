package experiment

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

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
		So(assertion(0.0, threshold), ShouldNotEqual, "")
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
