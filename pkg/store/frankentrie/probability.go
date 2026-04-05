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
	maximumSuffix := min(len(contextTokens), store.interpolationSuffixDepth)
	totalWeight := 0.0

	for suffixLength := 0; suffixLength <= maximumSuffix; suffixLength++ {
		suffix := contextTokens[len(contextTokens)-suffixLength:]
		node, _, _, matched := store.walkTokens(suffix, true)
		if !matched {
			continue
		}

		weight := math.Pow(2, float64(suffixLength))
		if store.linearInterpolation {
			weight = float64(suffixLength + 1)
		}

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
		}
	}

	if totalWeight == 0 {
		return store.blendEpisodicTail(contextTokens, label, probabilities)
	}

	for token, probability := range probabilities {
		probabilities[token] = probability / totalWeight
	}

	return store.blendEpisodicTail(contextTokens, label, probabilities)
}

func (store *Store) nextProbabilitiesFromTokens(tokens []string, label string, temperature float64) []NextProbability {
	if temperature < 0 {
		temperature = 0
	}

	probabilityMap := store.interpolatedProbabilities(tokens, label)
	if len(probabilityMap) == 0 {
		return nil
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
