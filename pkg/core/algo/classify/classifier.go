package classify

import (
	"math"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Classifier computes Bayesian label posteriors from token frequency
distributions built from the Prediction's Context values. The trie
Walk populates Context before Update is called — the Classifier
tracks per-label token profiles internally and scores incoming
observations against them.

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
Update receives Context values and Labels. For each label, it
accumulates the token frequencies from Context into that label's
profile. Then it classifies the current observation by computing
log-evidence for each known label and converting to posteriors.
*/
func (classifier *Classifier) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	classifier.mu.Lock()
	defer classifier.mu.Unlock()

	if prediction == nil || len(prediction.Context) == 0 {
		return classifier.prediction, nil
	}

	tokens := classifier.tokenize(prediction)

	for _, label := range prediction.Labels {
		name := string(label.Label)
		classifier.train(name, tokens)
	}

	classifier.observations++
	posteriors := classifier.classify(tokens)

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

	src := classifier.prediction

	out := &algo.Prediction{
		Labels:        make([]algo.Label, len(src.Labels)),
		Continuations: make([]algo.Continuation, len(src.Continuations)),
		Context:       append([]primitive.Value(nil), src.Context...),
		Signals:       make(map[algo.SignalType]*numeric.Derived, len(src.Signals)),
	}

	for idx, label := range src.Labels {
		out.Labels[idx] = algo.Label{
			Label:      append([]byte(nil), label.Label...),
			Confidence: label.Confidence,
		}
	}

	for idx, cont := range src.Continuations {
		out.Continuations[idx] = algo.Continuation{
			Sequence: append([]byte(nil), cont.Sequence...),
			Score:    cont.Score,
		}
	}

	for signalType, signal := range src.Signals {
		if signal == nil {
			continue
		}

		out.Signals[signalType] = signal.Clone()
	}

	return out
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
