package frankentrie

import (
	"fmt"
	"math"
	"strings"
)

/*
Train records one labeled sequence scaled by learningRate, which implements
surprise-modulated plasticity when driven through Experience.

Pruning and ExtractPatterns run every pruneInterval steps; call Flush after a
batch of trains when consumers need an up-to-date pattern list sooner.
*/
func (store *Store) Train(sequence string, label string, learningRate float64) {
	label = strings.TrimSpace(label)
	if label == "" || learningRate <= 0 {
		return
	}

	store.addLabel(label)
	store.currentStep++
	store.patternsDirty = true

	decay := store.decayFactor
	if store.adaptive != nil {
		decay = store.adaptive.adaptiveDecayFactor(decay)
	}

	for _, knownLabel := range store.labels {
		store.ClassTotals[knownLabel] *= decay
	}

	store.ClassTotals[label] += learningRate

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

	if store.pruneInterval > 0 && store.currentStep%store.pruneInterval == 0 {
		store.applyPrune()
		store.rebuildExtractedPatterns()
	}
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

	// Feed surprisal observations into the adaptive state.
	for _, item := range series {
		if store.adaptive != nil {
			store.adaptive.observeSurprisal(item.Bits)
		}
	}

	surprisalScale := defaultSurprisalScaleBits
	if store.adaptive != nil {
		surprisalScale = store.adaptive.adaptiveSurprisalScale()
	}

	learningRate := math.Min(
		defaultMaxLearningRate,
		defaultBaselineLearningRate+averageBits/surprisalScale,
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

			if store.adaptive != nil {
				threshold = store.adaptive.adaptiveUnsupervisedThreshold(threshold)
			}

			if maxScore < threshold {
				label = store.nextConceptLabel()
				isNewConcept = true
			} else {
				label = bestLabel
			}

			// Track accuracy: did the label we picked stay dominant after retraining?
			if store.adaptive != nil && !isNewConcept {
				store.adaptive.observeClassifyAccuracy(true)
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
