package classify

import (
	"math"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
)

/*
Classifier computes Bayesian label posteriors from per-token log-probabilities
produced by a suffix-interpolated language model. The classifier itself holds
no state — it receives everything it needs per call.
*/
type Classifier struct {
	labels        []string
	classTotals   map[string]float64
	currentStep   int
	unknownFloor  float64
	contextTokens []string
	prediction    *algo.Prediction
}

func NewClassifier() *Classifier {
	return &Classifier{
		prediction: algo.NewPrediction(),
	}
}

func (classifier *Classifier) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	return prediction, nil
}

func (classifier *Classifier) Value() *algo.Prediction {
	return classifier.prediction
}

/*
LogEvidence computes per-label log-probability from token-level conditionals.

tokenProbabilities is called for each (tokenIndex, label) pair and returns
P(token | context, label). unknownFloor replaces zero probabilities.
classTotal maps labels to their accumulated class mass.
currentStep is the global training step for the prior.
*/
func (classifier *Classifier) LogEvidence(
	labels []string,
	tokens []string,
	classificationContext int,
	classTotals map[string]float64,
	currentStep int,
	unknownFloor float64,
	tokenProbabilities func(contextTokens []string, label string) map[string]float64,
) (logEvidence map[string]float64, contributions map[string][]TokenContribution) {
	logEvidence = make(map[string]float64, len(labels))
	contributions = make(map[string][]TokenContribution, len(labels))

	for _, label := range labels {
		logEvidence[label] = 0
		contributions[label] = nil
	}

	if len(labels) == 0 {
		return logEvidence, contributions
	}

	for _, label := range labels {
		classTotal := classTotals[label]

		if classTotal == 0 {
			classTotal = 0.1
		}

		logProbability := math.Log(classTotal / math.Max(float64(currentStep), 1))
		trace := []TokenContribution{
			{Token: "PRIOR", LogProb: logProbability},
		}

		for tokenIndex := range tokens {
			contextStart := tokenIndex - classificationContext

			if contextStart < 0 {
				contextStart = 0
			}

			contextTokens := tokens[contextStart:tokenIndex]
			probabilities := tokenProbabilities(contextTokens, label)
			tokenProbability := probabilities[tokens[tokenIndex]]

			if tokenProbability <= 0 {
				tokenProbability = unknownFloor
			}

			lp := math.Log(tokenProbability)
			logProbability += lp
			trace = append(trace, TokenContribution{
				Token:   tokens[tokenIndex],
				LogProb: lp,
			})
		}

		logEvidence[label] = logProbability
		contributions[label] = trace
	}

	return logEvidence, contributions
}

/*
Posteriors converts log-evidence into softmax percentage scores.
*/
func (classifier *Classifier) Posteriors(
	logEvidence map[string]float64,
	labels []string,
) map[string]float64 {
	return numeric.SoftmaxPercentages(logEvidence, labels)
}

/*
TokenContribution is one step in the per-label log-prob trace.
*/
type TokenContribution struct {
	Token   string
	LogProb float64
}
