package frankentrie

import (
	"math"
	"sort"
)

/*
NextProbability holds one next-token probability.
*/
type NextProbability struct {
	Token       string
	Probability float64
}

func (store *Store) interpolatedProbabilities(contextTokens []string, label string) map[string]float64 {
	probabilities := make(map[string]float64)
	effectiveDepth := store.interpolationSuffixDepth
	if store.adaptive != nil {
		effectiveDepth = store.adaptive.adaptiveInterpolationDepth(effectiveDepth)
	}

	maximumSuffix := min(len(contextTokens), effectiveDepth)
	totalWeight := 0.0
	bestDepth := 0
	bestDepthProb := 0.0

	for suffixLength := 0; suffixLength <= maximumSuffix; suffixLength++ {
		suffix := contextTokens[len(contextTokens)-suffixLength:]
		node, _, _, matched := store.walkTokens(suffix, true)
		if !matched {
			continue
		}

		weight := store.adaptive.interpolationWeight(suffixLength, store.linearInterpolation)
		totalWeight += weight

		nodeTotal := 0.0
		childTokens := sortedChildTokens(node)
		for _, childToken := range childTokens {
			nodeTotal += store.EffectiveCount(node.Children[childToken], label)
		}

		vocabularySize := len(childTokens)
		if vocabularySize == 0 {
			vocabularySize = 1
		}

		for _, childToken := range childTokens {
			count := store.EffectiveCount(node.Children[childToken], label)
			probability := (count + defaultAdditiveSmoothing) / (nodeTotal + defaultAdditiveSmoothing*float64(vocabularySize))
			probabilities[childToken] += probability * weight
			if probability > bestDepthProb {
				bestDepthProb = probability
				bestDepth = suffixLength
			}
		}
	}

	// Feed the winning depth back to the adaptive state.
	if store.adaptive != nil && totalWeight > 0 {
		store.adaptive.observeInterpolationHit(bestDepth)
	}

	if totalWeight == 0 {
		return store.blendEpisodicTail(contextTokens, label, probabilities)
	}

	for token, probability := range probabilities {
		probabilities[token] = probability / totalWeight
	}

	return store.blendEpisodicTail(contextTokens, label, probabilities)
}

/*
NextProbabilities returns temperature-scaled next-token rankings for context,
mirroring the demo getNextProbabilities API.
*/
func (store *Store) NextProbabilities(
	context string, label string, temperature float64,
) []NextProbability {
	return store.nextProbabilitiesFromTokens(store.Tokenize(context), label, temperature)
}

func (store *Store) nextProbabilitiesFromTokens(tokens []string, label string, temperature float64) []NextProbability {
	if temperature < 0 {
		temperature = 0
	}

	probabilityMap := store.interpolatedProbabilities(tokens, label)
	if len(probabilityMap) == 0 {
		return nil
	}

	if store.adaptive != nil && temperature > 0 {
		temperature = store.adaptive.adaptiveTemperature(temperature, probabilityMap)
	}

	probabilities := make([]NextProbability, 0, len(probabilityMap))
	for token, probability := range probabilityMap {
		probabilities = append(probabilities, NextProbability{
			Token:       token,
			Probability: probability,
		})
	}

	if temperature == 0 {
		maxProbability := probabilities[0].Probability
		for _, probability := range probabilities[1:] {
			if probability.Probability > maxProbability {
				maxProbability = probability.Probability
			}
		}

		bestCount := 0
		for _, probability := range probabilities {
			if probability.Probability == maxProbability {
				bestCount++
			}
		}

		for probabilityIndex := range probabilities {
			if probabilities[probabilityIndex].Probability == maxProbability {
				probabilities[probabilityIndex].Probability = 1 / float64(bestCount)
				continue
			}

			probabilities[probabilityIndex].Probability = 0
		}

		sortProbabilities(probabilities)
		return probabilities
	}

	sumProbability := 0.0
	for probabilityIndex := range probabilities {
		probabilities[probabilityIndex].Probability = math.Pow(probabilities[probabilityIndex].Probability, 1/temperature)
		sumProbability += probabilities[probabilityIndex].Probability
	}

	if sumProbability == 0 {
		return nil
	}

	for probabilityIndex := range probabilities {
		probabilities[probabilityIndex].Probability /= sumProbability
	}

	sortProbabilities(probabilities)
	return probabilities
}

func sortProbabilities(probabilities []NextProbability) {
	sort.Slice(probabilities, func(leftIndex int, rightIndex int) bool {
		left := probabilities[leftIndex]
		right := probabilities[rightIndex]

		if left.Probability == right.Probability {
			return left.Token < right.Token
		}

		return left.Probability > right.Probability
	})
}
