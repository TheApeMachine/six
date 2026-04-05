package frankentrie

import (
	"fmt"
	"math"
	"strings"
)

/*
Train records one labeled sequence scaled by learningRate, which implements
surprise-modulated plasticity when driven through Experience.
*/
func (store *Store) Train(sequence string, label string, learningRate float64) {
	label = strings.TrimSpace(label)
	if label == "" || learningRate <= 0 {
		return
	}

	store.addLabel(label)
	store.currentStep++

	for _, knownLabel := range store.labels {
		store.classTotals[knownLabel] *= store.decayFactor
	}

	store.classTotals[label] += learningRate

	tokens := store.tokensWithEnd(sequence)
	store.pushEpisodic(tokens, label)

	words := store.contentTokens(tokens)
	store.updateVocabulary(words)
	store.updateCoOccurrence(words)

	for startIndex := range tokens {
		node := store.root
		store.touchNode(node, label, learningRate)

		endIndex := min(len(tokens), startIndex+store.maximumPathLength)
		for tokenIndex := startIndex; tokenIndex < endIndex; tokenIndex++ {
			token := tokens[tokenIndex]
			child := node.Children[token]
			if child == nil {
				store.nodeCount++
				child = &Node{
					ID:             fmt.Sprintf("node_%d", store.nodeCount),
					Token:          token,
					Children:       make(map[string]*Node),
					ClassCounts:    make(map[string]float64),
					Depth:          node.Depth + 1,
					LastUpdateStep: store.currentStep,
				}
				node.Children[token] = child
			}

			node = child
			store.touchNode(node, label, learningRate)
		}
	}

	store.prune()
	store.ExtractPatterns()
}

/*
Experience performs unsupervised or supervised learning with automatic concept
spawning and surprise-modulated learning rates.
*/
func (store *Store) Experience(sequence string, providedLabel *string) ExperienceResult {
	result := ExperienceResult{
		Label:        defaultExperienceEmptyLabel,
		Surprisal:    0,
		LearningRate: 0,
	}

	if store == nil {
		return result
	}

	content := store.contentTokens(store.Tokenize(sequence))
	if len(content) == 0 {
		return result
	}

	series := store.SurprisalSeries(sequence)
	totalBits := 0.0

	for _, item := range series {
		totalBits += item.Bits
	}

	averageBits := 0.0
	if len(series) > 0 {
		averageBits = totalBits / float64(len(series))
	}

	learningRate := math.Min(
		defaultMaxLearningRate,
		defaultBaselineLearningRate+averageBits/defaultSurprisalScaleBits,
	)

	label := ""
	isNewConcept := false

	if providedLabel != nil {
		label = strings.TrimSpace(*providedLabel)
	}

	if label == "" {
		if len(store.labels) == 0 {
			label = store.nextConceptLabel()
			isNewConcept = true
		} else {
			scores := store.Classify(sequence)
			bestLabel, maxScore := store.bestLabelScore(scores)
			threshold := store.unsupervisedThreshold
			if threshold <= 0 {
				threshold = defaultUnsupervisedConfidence
			}

			if maxScore < threshold {
				label = store.nextConceptLabel()
				isNewConcept = true
			} else {
				label = bestLabel
			}
		}
	}

	store.Train(sequence, label, learningRate)

	result.Label = label
	result.Surprisal = averageBits
	result.LearningRate = learningRate
	result.IsNewConcept = isNewConcept

	return result
}
