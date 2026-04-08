package policy

import (
	"cmp"
	"slices"

	"github.com/theapemachine/six/pkg/core/algo"
)

/*
ActionScore is one action candidate with its aggregated control score and
support mass. Score is the expected value projected from lower layers;
Support tracks how much evidence contributed to that score.
*/
type ActionScore struct {
	Action  string
	Score   float64
	Support float64
}

/*
ActionScores is a sortable projection of policy candidates.
*/
type ActionScores []ActionScore

/*
NewActionScores lifts score and support maps into a sortable ActionScores slice.
Empty action keys are ignored.
*/
func NewActionScores(
	scores map[string]float64,
	support map[string]float64,
) ActionScores {
	out := make(ActionScores, 0, len(scores))

	for action, score := range scores {
		if action == "" {
			continue
		}

		out = append(out, ActionScore{
			Action:  action,
			Score:   score,
			Support: support[action],
		})
	}

	return out
}

/*
SortDescending orders highest-score actions first, breaking ties by support and
then lexicographic action text so equal-mass runs stay deterministic.
*/
func (scores ActionScores) SortDescending() {
	slices.SortFunc(scores, func(left, right ActionScore) int {
		if left.Score != right.Score {
			return cmp.Compare(right.Score, left.Score)
		}

		if left.Support != right.Support {
			return cmp.Compare(right.Support, left.Support)
		}

		return cmp.Compare(left.Action, right.Action)
	})
}

/*
Prediction projects ActionScores into the shared algo.Prediction envelope so
control output can compose upward through the same continuation channel as beam.
*/
func (scores ActionScores) Prediction() *algo.Prediction {
	prediction := algo.NewPrediction()

	for _, score := range scores {
		prediction.Continuations = append(prediction.Continuations, algo.Continuation{
			Sequence: []byte(score.Action),
			Score:    score.Score,
		})
	}

	return prediction
}
