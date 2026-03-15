package experiment

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
	data.Scores = ByteScores(data.Holdout, data.Observed)

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
