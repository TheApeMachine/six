package markovtrie

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
	if store == nil {
		return
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	store.trainBody(sequence, label, learningRate)
}

/*
trainBody contains Train's mutable work; caller must hold store.mu.
*/
func (store *Store) trainBody(sequence string, label string, learningRate float64) {
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
	if store == nil {
		return ExperienceResult{
			Label:        defaultExperienceEmptyLabel,
			Surprisal:    0,
			LearningRate: 0,
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.experienceBody(sequence, providedLabel)
}

/*
experienceBody is the Experience implementation; caller must hold store.mu.
*/
func (store *Store) experienceBody(sequence string, providedLabel *string) ExperienceResult {
	result := ExperienceResult{
		Label:        defaultExperienceEmptyLabel,
		Surprisal:    0,
		LearningRate: 0,
	}

	content := store.contentTokens(store.tokenizeUnlocked(sequence))
	if len(content) == 0 {
		return result
	}

	series := store.surprisalSeriesBody(sequence)
	totalBits := 0.0

	for _, item := range series {
		totalBits += item.Bits
	}

	averageBits := 0.0
	if len(series) > 0 {
		averageBits = totalBits / float64(len(series))
	}

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
	observeAccuracy := false

	if providedLabel != nil {
		label = strings.TrimSpace(*providedLabel)
	}

	if label == "" {
		if len(store.labels) == 0 {
			label = store.nextConceptLabel()
			isNewConcept = true
		} else {
			scores := store.classifyBody(sequence)
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
				if store.adaptive != nil {
					observeAccuracy = true
				}
			}
		}
	}

	store.trainBody(sequence, label, learningRate)

	if observeAccuracy {
		scoresAfter := store.classifyBody(sequence)
		postBest, _ := store.bestLabelScore(scoresAfter)
		store.adaptive.observeClassifyAccuracy(label == postBest)
	}

	result.Label = label
	result.Surprisal = averageBits
	result.LearningRate = learningRate
	result.IsNewConcept = isNewConcept

	return result
}
