package frankentrie

import (
	"math"
	"strings"
	"unicode/utf8"
)

/*
ContextMatch describes the deepest
token path matched for a context.
*/
type ContextMatch struct {
	Node          *Node
	ActiveTokens  []string
	ActiveContext string
	Path          []*Node
}

/*
SemanticMatch reports how one token was
mapped through the learned vocabulary.
*/
type SemanticMatch struct {
	Original   string
	Mapped     string
	Similarity float64
}

/*
MatchContext returns the longest token suffix that can be walked in the trie,
allowing single-edit fuzzy matches at the token level.
*/
func (store *Store) MatchContext(context string) ContextMatch {
	tokens := store.Tokenize(context)

	fallbackMatch := ContextMatch{
		Node: store.root,
		Path: []*Node{store.root},
	}

	var bestMatch ContextMatch
	bestTokenCount := -1

	for startIndex := 0; startIndex < len(tokens); startIndex++ {
		suffix := tokens[startIndex:]
		node, path, matchedTokens, matched := store.walkTokens(suffix, true)
		if !matched {
			continue
		}

		if len(matchedTokens) > bestTokenCount {
			bestTokenCount = len(matchedTokens)
			bestMatch = ContextMatch{
				Node:          node,
				ActiveTokens:  matchedTokens,
				ActiveContext: strings.Join(matchedTokens, ""),
				Path:          path,
			}
		}
	}

	if bestTokenCount >= 0 {
		return bestMatch
	}

	return fallbackMatch
}

/*
Similarity returns cosine similarity between two co-occurrence rows.
*/
func (store *Store) Similarity(
	leftToken string, rightToken string,
) float64 {
	if leftToken == rightToken {
		return 1
	}

	leftVector := store.coOccurrence[leftToken]
	rightVector := store.coOccurrence[rightToken]

	if leftVector == nil || rightVector == nil {
		return 0
	}

	dotProduct := 0.0
	leftMagnitude := 0.0
	rightMagnitude := 0.0

	for token, count := range leftVector {
		dotProduct += count * rightVector[token]
		leftMagnitude += count * count
	}

	for _, count := range rightVector {
		rightMagnitude += count * count
	}

	if leftMagnitude == 0 || rightMagnitude == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(leftMagnitude) * math.Sqrt(rightMagnitude))
}

/*
EditDistance returns Levenshtein distance between two tokens.
*/
func (store *Store) EditDistance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)

	matrix := make([][]int, len(leftRunes)+1)
	for rowIndex := range matrix {
		matrix[rowIndex] = make([]int, len(rightRunes)+1)
	}

	for rowIndex := 0; rowIndex <= len(leftRunes); rowIndex++ {
		matrix[rowIndex][0] = rowIndex
	}

	for columnIndex := 0; columnIndex <= len(rightRunes); columnIndex++ {
		matrix[0][columnIndex] = columnIndex
	}

	for rowIndex := 1; rowIndex <= len(leftRunes); rowIndex++ {
		for columnIndex := 1; columnIndex <= len(rightRunes); columnIndex++ {
			cost := 0
			if leftRunes[rowIndex-1] != rightRunes[columnIndex-1] {
				cost = 1
			}

			matrix[rowIndex][columnIndex] = min(
				min(matrix[rowIndex-1][columnIndex]+1, matrix[rowIndex][columnIndex-1]+1),
				matrix[rowIndex-1][columnIndex-1]+cost,
			)
		}
	}

	return matrix[len(leftRunes)][len(rightRunes)]
}

/*
SemanticEquivalent maps a token onto the nearest known vocabulary item using
edit distance first and co-occurrence similarity second.
*/
func (store *Store) SemanticEquivalent(word string) SemanticMatch {
	if _, exists := store.vocabulary[word]; exists {
		return SemanticMatch{
			Original:   word,
			Mapped:     word,
			Similarity: 1,
		}
	}

	bestWord := word
	bestSimilarity := -1.0
	editMapped := ""
	editDistance := int(^uint(0) >> 1)

	for _, knownWord := range store.vocabularyOrder {
		if absInt(utf8.RuneCountInString(knownWord)-utf8.RuneCountInString(word)) <= 2 {
			distance := store.EditDistance(word, knownWord)
			if distance <= defaultEditDistance {
				if distance < editDistance {
					editDistance = distance
					editMapped = knownWord
				}

				continue
			}
		}

		similarity := store.Similarity(word, knownWord)
		if similarity <= bestSimilarity {
			continue
		}

		bestSimilarity = similarity
		bestWord = knownWord
	}

	if editMapped != "" {
		return SemanticMatch{
			Original:   word,
			Mapped:     editMapped,
			Similarity: defaultEditSimilarity,
		}
	}

	if bestSimilarity > 0 {
		return SemanticMatch{
			Original:   word,
			Mapped:     bestWord,
			Similarity: bestSimilarity,
		}
	}

	return SemanticMatch{
		Original:   word,
		Mapped:     word,
		Similarity: 1,
	}
}

/*
AttentionContext returns semantic mappings for content tokens in the context.
*/
func (store *Store) AttentionContext(context string) []SemanticMatch {
	tokens := store.contentTokens(store.Tokenize(context))
	matches := make([]SemanticMatch, 0, len(tokens))

	for _, token := range tokens {
		matches = append(matches, store.SemanticEquivalent(token))
	}

	return matches
}

/*
InterpolatedProbabilities returns weighted next-token probabilities from all
suffixes up to the configured interpolation depth.
*/
func (store *Store) InterpolatedProbabilities(context string, label string) map[string]float64 {
	return store.interpolatedProbabilities(store.Tokenize(context), label)
}

/*
SurprisalSeries returns the surprisal for each token in the context.
*/
func (store *Store) SurprisalSeries(context string) []Surprisal {
	tokens := store.Tokenize(context)
	surprisals := make([]Surprisal, 0, len(tokens))

	for tokenIndex := range tokens {
		contextStart := tokenIndex - store.classificationContext
		if contextStart < 0 {
			contextStart = 0
		}

		contextTokens := tokens[contextStart:tokenIndex]
		probabilities := store.interpolatedProbabilities(contextTokens, "")
		tokenProbability := probabilities[tokens[tokenIndex]]
		// Token absent from interpolation yields 0; log2(0) is -Inf and smoothing avoids NaN.
		// defaultUnknownProbability is a small floor (~10^-3); we assume unknown/rare mass is
		// rare but not zero so surprisal stays finite and comparable across labels.
		if tokenProbability <= 0 {
			tokenProbability = defaultUnknownProbability
		}

		surprisals = append(surprisals, Surprisal{
			Token: tokens[tokenIndex],
			Bits:  -math.Log2(tokenProbability),
		})
	}

	return surprisals
}
