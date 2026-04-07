package surprisal

import (
	"math"
	"strings"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
)

/*
Probability computes surprisal from the token frequency distribution
observed in the Prediction's Context. The trie Walk populates Context
with every node's Value before Update is called — this algorithm
builds a frequency table from those Values and measures how surprising
the new observation (the last Context entry) is relative to that
distribution.

Signals produced:
  - Surprisal: EMA-smoothed surprisal in bits
*/
type Probability struct {
	prediction *algo.Prediction
	surprisal  *numeric.Derived
}

/*
NewProbability constructs a surprisal algorithm.
*/
func NewProbability() *Probability {
	surprisal := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	prediction := algo.NewPrediction()
	prediction.Signals[algo.Surprisal] = surprisal

	return &Probability{
		prediction: prediction,
		surprisal:  surprisal,
	}
}

/*
Update builds a token frequency distribution from Context values and
computes the mean surprisal of the newest observation against it.
*/
func (probability *Probability) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if prediction == nil || len(prediction.Context) < 2 {
		return probability.prediction, nil
	}

	context := prediction.Context
	freq := make(map[string]float64)
	var total float64

	for _, value := range context[:len(context)-1] {
		for token := range strings.FieldsSeq(value.String()) {
			freq[token]++
			total++
		}
	}

	if total == 0 {
		return probability.prediction, nil
	}

	vocabSize := float64(len(freq))
	last := context[len(context)-1]

	var bits float64
	var tokenCount float64

	for token := range strings.FieldsSeq(last.String()) {
		count := freq[token]
		p := (count + 1.0) / (total + vocabSize)

		bits += -math.Log2(p)
		tokenCount++
	}

	if tokenCount > 0 {
		probability.surprisal.Next(bits / tokenCount)
	}

	return probability.prediction, nil
}

func (probability *Probability) Value() *algo.Prediction {
	return probability.prediction
}
