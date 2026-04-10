package experiment

import gc "github.com/smartystreets/goconvey/convey"

/*
EvaluatorOutcomeForPrompt builds the Convey triple for table row idx using
the same metric scale and threshold as Outcome(), not the running mean over
all rows. See Evaluator.OutcomeForTableRow.
*/
func EvaluatorOutcomeForPrompt(
	evaluator *Evaluator,
	table []ExperimentalData,
	idx int,
) (any, gc.Assertion, any) {
	return evaluator.OutcomeForTableRow(table, idx)
}
