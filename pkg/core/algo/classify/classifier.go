package classify

import (
	"math"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
)

/*
Classifier computes Bayesian label posteriors from token frequency
distributions built from the Prediction's Context values. The trie
Walk populates Context before Update is called — the Classifier
tracks per-label token profiles internally and scores incoming
observations against them.

Recursive path:
  - At the trie level, the classifier infers labels from Context values.
  - At the node level, the same classifier composes child label evidence
    from prediction.Labels into a consensus posterior.

Targets are supervision only. Incoming Labels are evidence from layers below.

Signals produced:
  - Entropy: EMA-smoothed Shannon entropy of the posterior distribution.
    High entropy means the classifier is uncertain across labels;
    low entropy means one label dominates.
  - Accuracy: EMA-smoothed classification confidence of the top label.
*/
type Classifier struct {
	mu            sync.Mutex
	prediction    *algo.Prediction
	entropy       *numeric.Derived
	accuracy      *numeric.Derived
	labelProfiles map[string]map[string]float64
	labelTotals   map[string]float64
	observations  float64
}

/*
NewClassifier constructs a classifier with EMA-smoothed signal chains.
*/
func NewClassifier() *Classifier {
	entropy := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	accuracy := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	prediction := algo.NewPrediction()
	prediction.Signals[algo.Entropy] = entropy
	prediction.Signals[algo.Accuracy] = accuracy

	return &Classifier{
		prediction:    prediction,
		entropy:       entropy,
		accuracy:      accuracy,
		labelProfiles: make(map[string]map[string]float64),
		labelTotals:   make(map[string]float64),
	}
}

/*
Update receives Context values, Targets, and child Labels. For each target, it
accumulates the token frequencies from Context into the target label's
profile. Then it classifies the current observation from Context,
incoming child Labels, or both.
*/
func (classifier *Classifier) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if prediction == nil {
		return classifier.prediction, nil
	}

	tokens := classifier.tokenize(prediction)
	targets := prediction.SupervisionLabels()

	classifier.mu.Lock()
	defer classifier.mu.Unlock()

	for _, label := range targets {
		name := string(label.Label)
		classifier.train(name, tokens)
	}

	if len(targets) > 0 && len(tokens) > 0 {
		classifier.observations++
	}

	posteriors := classifier.compose(
		classifier.classify(tokens),
		classifier.classifyLabels(prediction.Labels),
	)

	if len(posteriors) == 0 {
		return classifier.prediction, nil
	}

	classifier.prediction.Labels = classifier.prediction.Labels[:0]
	var bestConf float64

	for label, conf := range posteriors {
		classifier.prediction.Labels = append(
			classifier.prediction.Labels,
			algo.Label{Label: []byte(label), Confidence: conf},
		)

		if conf > bestConf {
			bestConf = conf
		}
	}

	classifier.accuracy.Next(bestConf)
	classifier.entropy.Next(shannonEntropy(posteriors))

	return classifier.prediction, nil
}

func (classifier *Classifier) Value() *algo.Prediction {
	classifier.mu.Lock()
	defer classifier.mu.Unlock()

	if classifier.prediction == nil {
		return nil
	}

	return classifier.prediction.Clone()
}

/*
train accumulates token frequencies into a label's profile.
*/
func (classifier *Classifier) train(
	label string, tokens []string,
) {
	profile, exists := classifier.labelProfiles[label]

	if !exists {
		profile = make(map[string]float64)
		classifier.labelProfiles[label] = profile
	}

	for _, token := range tokens {
		profile[token]++
		classifier.labelTotals[label]++
	}
}

/*
classify computes log-evidence for each known label against the
given tokens and returns softmax posteriors.
*/
func (classifier *Classifier) classify(
	tokens []string,
) map[string]float64 {
	if len(classifier.labelProfiles) == 0 || len(tokens) == 0 {
		return nil
	}

	logEvidence := make(map[string]float64, len(classifier.labelProfiles))

	for label, profile := range classifier.labelProfiles {
		total := classifier.labelTotals[label]

		if total == 0 {
			continue
		}

		logPrior := math.Log(total / math.Max(classifier.observations, 1))
		logProb := logPrior

		vocabSize := float64(len(profile))

		for _, token := range tokens {
			count := profile[token]
			prob := (count + 1.0) / (total + 1.0*vocabSize)
			logProb += math.Log(prob)
		}

		logEvidence[label] = logProb
	}

	return softmax(logEvidence)
}

/*
classifyLabels composes child classifier output into a parent posterior by
aggregating confidences per label and normalizing them.
*/
func (classifier *Classifier) classifyLabels(
	labels []algo.Label,
) map[string]float64 {
	if len(labels) == 0 {
		return nil
	}

	posteriors := make(map[string]float64)
	total := 0.0

	for _, label := range labels {
		name := string(label.Label)

		if name == "" {
			continue
		}

		weight := label.Confidence

		if weight <= 0 {
			continue
		}

		posteriors[name] += weight
		total += weight
	}

	if total <= 0 {
		return nil
	}

	for name, weight := range posteriors {
		posteriors[name] = weight / total
	}

	return posteriors
}

/*
compose combines local token posteriors with child-label posteriors.
When both are present, their normalized masses are added and then
renormalized, preserving both local evidence and lower-layer consensus.
*/
func (classifier *Classifier) compose(
	local map[string]float64,
	child map[string]float64,
) map[string]float64 {
	if len(local) == 0 {
		return child
	}

	if len(child) == 0 {
		return local
	}

	combined := make(map[string]float64, len(local)+len(child))
	total := 0.0

	for label, prob := range local {
		combined[label] += prob
		total += prob
	}

	for label, prob := range child {
		combined[label] += prob
		total += prob
	}

	if total <= 0 {
		return nil
	}

	for label, prob := range combined {
		combined[label] = prob / total
	}

	return combined
}

/*
tokenize extracts tokens from all Context values.
*/
func (classifier *Classifier) tokenize(
	prediction *algo.Prediction,
) []string {
	var tokens []string

	for _, value := range prediction.Context {
		tokens = append(tokens, strings.Fields(value.String())...)
	}

	return tokens
}

/*
shannonEntropy computes -Σ p·log2(p) from a probability distribution.
*/
func shannonEntropy(probs map[string]float64) float64 {
	var entropy float64

	for _, prob := range probs {
		if prob > 0 {
			entropy -= prob * math.Log2(prob)
		}
	}

	return entropy
}

/*
softmax converts log-evidence values to a normalized probability
distribution using the log-sum-exp trick for numerical stability.
*/
func softmax(logEvidence map[string]float64) map[string]float64 {
	if len(logEvidence) == 0 {
		return nil
	}

	maxLog := math.Inf(-1)

	for _, logProb := range logEvidence {
		if logProb > maxLog {
			maxLog = logProb
		}
	}

	var sumExp float64
	posteriors := make(map[string]float64, len(logEvidence))

	for label, logProb := range logEvidence {
		exp := math.Exp(logProb - maxLog)
		posteriors[label] = exp
		sumExp += exp
	}

	if sumExp > 0 {
		for label := range posteriors {
			posteriors[label] /= sumExp
		}
	}

	return posteriors
}
