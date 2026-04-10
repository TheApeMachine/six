package experiment

import "strings"

/*
ExtractionScorer scores by exact word match only — no substring credit.
Used for answer-extraction tasks (e.g. bAbI) where the observed output
must be the answer word itself, not a paragraph that happens to contain it.
*/
type ExtractionScorer struct{}

func (scorer *ExtractionScorer) Enrich(data *ExperimentalData) {
	exp := strings.TrimSpace(strings.ToLower(string(data.Holdout)))

	obs := strings.TrimSpace(strings.ToLower(string(data.Generation)))

	var exact float64

	if exp != "" && obs == exp {
		exact = 1.0
	}

	data.Scores = Scores{Exact: exact, Partial: exact, Fuzzy: exact}
	data.WeightedTotal = exact
}

func (scorer *ExtractionScorer) Aggregate(data []ExperimentalData) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, row := range data {
		sum += row.WeightedTotal
	}
	return sum / float64(len(data))
}

func (scorer *ExtractionScorer) GateScore(row ExperimentalData) float64 {
	return row.WeightedTotal
}

func EvalWithExtractionScorer() evalOpts {
	return EvalWithScorer(&ExtractionScorer{})
}

/*
Scorer captures the per-result enrichment and aggregate score
computation strategy. Each experiment category plugs in its own
implementation, but the pipeline and Evaluator treat them uniformly.
*/
type Scorer interface {
	/*
		Enrich populates the derived fields on a single ExperimentalData row
		(Scores, WeightedTotal, etc.). Called once per result in AddResult.
	*/
	Enrich(data *ExperimentalData)

	/*
		Aggregate computes a single summary score over all collected results.
	*/
	Aggregate(data []ExperimentalData) float64

	/*
		GateScore is the per-row scalar aligned with Aggregate: what a single
		sample contributes to the gate’s expectation scale (e.g. WeightedTotal,
		Scores.Exact). Rows are assumed already Enrich’d.
	*/
	GateScore(row ExperimentalData) float64
}

/*
HoldoutScorer handles the standard byte-level holdout evaluation
used by the majority of experiments. Per-result enrichment computes
exact/partial/fuzzy byte scores and their weighted total. The aggregate
is the mean WeightedTotal.
*/
type HoldoutScorer struct{}

/*
Enrich computes byte-level scores from Holdout vs Observed.
*/
func (scorer *HoldoutScorer) Enrich(data *ExperimentalData) {
	data.Scores = ByteScores(data.Holdout, data.Generation)

	data.WeightedTotal = WeightedTotal(
		data.Scores.Exact,
		data.Scores.Partial,
		data.Scores.Fuzzy,
	)
}

/*
Aggregate returns the mean WeightedTotal across all results.
*/
func (scorer *HoldoutScorer) Aggregate(data []ExperimentalData) float64 {
	if len(data) == 0 {
		return 0
	}

	sum := 0.0

	for _, row := range data {
		sum += row.WeightedTotal
	}

	return sum / float64(len(data))
}

func (scorer *HoldoutScorer) GateScore(row ExperimentalData) float64 {
	return row.WeightedTotal
}

/*
HoldoutExactMeanScorer runs the same byte-level Enrich as HoldoutScorer so each
row still carries Exact, Partial, Fuzzy, and WeightedTotal for tables and charts.
Aggregate returns the mean Exact score only — matching the headline “Exact”
bars in paper figures and avoiding inflated pipeline gates from partial/fuzzy
credit that random-byte baselines were tuned for on WeightedTotal.
*/
type HoldoutExactMeanScorer struct{}

func (HoldoutExactMeanScorer) Enrich(data *ExperimentalData) {
	var hold HoldoutScorer

	hold.Enrich(data)
}

func (HoldoutExactMeanScorer) Aggregate(data []ExperimentalData) float64 {
	if len(data) == 0 {
		return 0
	}

	sum := 0.0

	for _, row := range data {
		sum += row.Scores.Exact
	}

	return sum / float64(len(data))
}

func (HoldoutExactMeanScorer) GateScore(row ExperimentalData) float64 {
	return row.Scores.Exact
}

/*
ScalingInstrumentScorer gates scaling/benchmark experiments on whether the
pipeline produced a readout (or holds a Prediction), not on recovering a
synthetic holdout suffix. Rows emitted from Finalize carry only metrics in
Scores / WeightedTotal with empty Holdout and Generation; those are left
untouched so throughput and compression summaries stay intact.
*/
type ScalingInstrumentScorer struct{}

func (ScalingInstrumentScorer) Enrich(data *ExperimentalData) {
	if len(data.Holdout) == 0 && len(data.Generation) == 0 && !strings.HasPrefix(data.Name, "prompt_") {
		return
	}

	score := 0.0

	if len(data.Generation) > 0 || data.Prediction != nil {
		score = 1.0
	}

	data.Scores = Scores{Exact: score, Partial: score, Fuzzy: score}
	data.WeightedTotal = score
}

func (ScalingInstrumentScorer) Aggregate(data []ExperimentalData) float64 {
	if len(data) == 0 {
		return 0
	}

	sum := 0.0

	for _, row := range data {
		sum += row.WeightedTotal
	}

	return sum / float64(len(data))
}

func (ScalingInstrumentScorer) GateScore(row ExperimentalData) float64 {
	return row.WeightedTotal
}
