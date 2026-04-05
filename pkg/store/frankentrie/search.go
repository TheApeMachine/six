package frankentrie

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

func (store *Store) CurrentStep() int {
	return store.currentStep
}

/*
Generate samples a continuation for the supplied context and label.
*/
func (store *Store) Generate(context string, label string, temperature float64, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}

	if temperature < 0 {
		temperature = 0
	}

	contextTokens := store.Tokenize(context)
	resultTokens := append([]string(nil), contextTokens...)
	recentTokens := make([]string, 0, defaultRecentWindow)

	for tokenIndex := 0; tokenIndex < maxLength; tokenIndex++ {
		probabilities := store.nextProbabilitiesFromTokens(resultTokens, label, temperature)
		if len(probabilities) == 0 {
			break
		}

		probabilities = applyRecentPenalty(probabilities, recentTokens)
		nextToken := store.sampleToken(probabilities, temperature)
		if nextToken == "" || nextToken == store.endToken {
			break
		}

		resultTokens = append(resultTokens, nextToken)
		recentTokens = append(recentTokens, nextToken)
		if len(recentTokens) > defaultRecentWindow {
			recentTokens = recentTokens[1:]
		}
	}

	generated := resultTokens[len(contextTokens):]
	if store.generationTokenJoiner != "" {
		return strings.Join(generated, store.generationTokenJoiner)
	}

	return strings.Join(generated, "")
}

/*
BeamSearch returns the highest-scoring continuations under a fixed beam width.
*/
func (store *Store) BeamSearch(context string, label string, beamWidth int, maxLength int) []BeamCandidate {
	if beamWidth <= 0 || maxLength <= 0 {
		return nil
	}

	initialTokens := store.Tokenize(context)
	beams := []beamState{{
		Tokens: append([]string(nil), initialTokens...),
		Score:  0,
	}}

	for stepIndex := 0; stepIndex < maxLength; stepIndex++ {
		nextBeams := make([]beamState, 0, beamWidth*beamWidth)

		for _, beam := range beams {
			if len(beam.Tokens) > 0 && beam.Tokens[len(beam.Tokens)-1] == store.endToken {
				nextBeams = append(nextBeams, beam)
				continue
			}

			probabilities := store.nextProbabilitiesFromTokens(beam.Tokens, label, 1)
			if len(probabilities) == 0 {
				nextBeams = append(nextBeams, beam)
				continue
			}

			limit := min(beamWidth, len(probabilities))
			for probabilityIndex := 0; probabilityIndex < limit; probabilityIndex++ {
				probability := probabilities[probabilityIndex]
				nextBeams = append(nextBeams, beamState{
					Tokens: append(append([]string(nil), beam.Tokens...), probability.Token),
					Score:  beam.Score + math.Log(probability.Probability),
				})
			}
		}

		sort.Slice(nextBeams, func(leftIndex int, rightIndex int) bool {
			return nextBeams[leftIndex].Score > nextBeams[rightIndex].Score
		})

		if len(nextBeams) > beamWidth {
			nextBeams = nextBeams[:beamWidth]
		}

		beams = nextBeams
		if beamsClosed(beams, store.endToken) {
			break
		}
	}

	candidates := make([]BeamCandidate, 0, len(beams))
	for _, beam := range beams {
		generatedTokens := make([]string, 0, len(beam.Tokens))
		for _, token := range beam.Tokens[len(initialTokens):] {
			if token == store.endToken {
				continue
			}

			generatedTokens = append(generatedTokens, token)
		}

		candidates = append(candidates, BeamCandidate{
			Sequence: strings.Join(generatedTokens, store.generationTokenJoiner),
			Score:    beam.Score,
		})
	}

	return candidates
}

/*
ExtractPatterns returns the label-skewed repeated symbol list, rebuilding from
the trie only when training has invalidated the cache since the last rebuild.
*/
func (store *Store) ExtractPatterns() []ExtractedSymbol {
	if !store.patternsDirty {
		return append([]ExtractedSymbol(nil), store.extractedSymbols...)
	}

	return store.rebuildExtractedPatterns()
}

func (store *Store) rebuildExtractedPatterns() []ExtractedSymbol {
	candidates := make(map[string]map[string]float64)

	var traverse func(node *Node, path []string)
	traverse = func(node *Node, path []string) {
		if len(path) > 0 {
			symbol := strings.Join(path, "")
			if candidates[symbol] == nil {
				candidates[symbol] = make(map[string]float64)
			}

			for _, label := range store.labels {
				candidates[symbol][label] += store.EffectiveCount(node, label)
			}
		}

		for _, token := range sortedChildTokens(node) {
			traverse(node.Children[token], append(path, token))
		}
	}

	for _, token := range sortedChildTokens(store.root) {
		traverse(store.root.Children[token], []string{token})
	}

	scored := make([]ExtractedSymbol, 0, len(candidates))
	for symbol, counts := range candidates {
		total := 0.0
		for _, count := range counts {
			total += count
		}

		if total < defaultSymbolMinimumTotal {
			continue
		}

		for _, label := range store.labels {
			count := counts[label]
			if count == 0 {
				continue
			}

			score := count / total * math.Log1p(count) * math.Sqrt(float64(utf8.RuneCountInString(symbol)))
			if score <= defaultSymbolMinimumScore {
				continue
			}

			scored = append(scored, ExtractedSymbol{
				Symbol: symbol,
				Label:  label,
				Score:  score,
			})
		}
	}

	sort.Slice(scored, func(leftIndex int, rightIndex int) bool {
		left := scored[leftIndex]
		right := scored[rightIndex]
		if left.Score == right.Score {
			if left.Symbol == right.Symbol {
				return left.Label < right.Label
			}

			return left.Symbol < right.Symbol
		}

		return left.Score > right.Score
	})

	if len(scored) > defaultSymbolLimit {
		scored = scored[:defaultSymbolLimit]
	}

	store.extractedSymbols = append([]ExtractedSymbol(nil), scored...)
	store.patternsDirty = false

	return append([]ExtractedSymbol(nil), store.extractedSymbols...)
}

/*
PosteriorsOverTime returns the posterior distribution for the empty context and
after each additional token in the input.
*/
func (store *Store) PosteriorsOverTime(context string) []map[string]float64 {
	tokens := store.Tokenize(context)
	posteriors := make([]map[string]float64, 0, len(tokens)+1)

	currentContext := ""
	posteriors = append(posteriors, store.Classify(currentContext))
	for _, token := range tokens {
		currentContext += token
		posteriors = append(posteriors, store.Classify(currentContext))
	}

	return posteriors
}

func (store *Store) walkTokens(tokens []string, allowFuzzy bool) (*Node, []*Node, []string, bool) {
	node := store.root
	path := []*Node{store.root}
	matchedTokens := make([]string, 0, len(tokens))

	for _, token := range tokens {
		child := node.Children[token]
		if child != nil {
			node = child
			path = append(path, child)
			matchedTokens = append(matchedTokens, child.Token)
			continue
		}

		if !allowFuzzy {
			return node, path, matchedTokens, false
		}

		fuzzyChild := store.fuzzyChild(node, token)
		if fuzzyChild == nil {
			return node, path, matchedTokens, false
		}

		node = fuzzyChild
		path = append(path, fuzzyChild)
		matchedTokens = append(matchedTokens, fuzzyChild.Token)
	}

	return node, path, matchedTokens, true
}

func (store *Store) fuzzyChild(node *Node, token string) *Node {
	for _, childToken := range sortedChildTokens(node) {
		if store.EditDistance(token, childToken) <= defaultEditDistance {
			return node.Children[childToken]
		}
	}

	return nil
}

func (store *Store) sampleToken(probabilities []NextProbability, temperature float64) string {
	if len(probabilities) == 0 {
		return ""
	}

	if temperature == 0 {
		maxProbability := probabilities[0].Probability
		bestTokens := make([]string, 0, len(probabilities))
		for _, probability := range probabilities {
			if probability.Probability > maxProbability {
				maxProbability = probability.Probability
				bestTokens = bestTokens[:0]
			}

			if probability.Probability == maxProbability {
				bestTokens = append(bestTokens, probability.Token)
			}
		}

		if len(bestTokens) == 0 {
			return ""
		}

		return bestTokens[store.random.Intn(len(bestTokens))]
	}

	randomValue := store.random.Float64()
	runningProbability := 0.0
	for _, probability := range probabilities {
		runningProbability += probability.Probability
		if randomValue <= runningProbability {
			return probability.Token
		}
	}

	return probabilities[len(probabilities)-1].Token
}

func applyRecentPenalty(probabilities []NextProbability, recentTokens []string) []NextProbability {
	adjusted := make([]NextProbability, 0, len(probabilities))
	sumProbability := 0.0

	for _, probability := range probabilities {
		penalty := 1.0
		if slices.Contains(recentTokens, probability.Token) {
			penalty = defaultRecentPenalty
		}

		adjustedProbability := probability.Probability * penalty
		adjusted = append(adjusted, NextProbability{
			Token:       probability.Token,
			Probability: adjustedProbability,
		})
		sumProbability += adjustedProbability
	}

	if sumProbability == 0 {
		return probabilities
	}

	for probabilityIndex := range adjusted {
		adjusted[probabilityIndex].Probability /= sumProbability
	}

	return adjusted
}

/*
EffectiveCount returns the node count after applying lazy decay from the last
update step. When label is empty, it returns the total visit count.
*/
func (store *Store) EffectiveCount(node *Node, label string) float64 {
	if node == nil {
		return 0
	}

	stepDelta := store.currentStep - node.LastUpdateStep
	decay := math.Pow(store.decayFactor, float64(stepDelta))

	if label != "" {
		return node.ClassCounts[label] * decay
	}

	return node.TotalVisits * decay
}
