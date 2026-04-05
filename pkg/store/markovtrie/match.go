package markovtrie

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
	if store == nil {
		return ContextMatch{}
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	tokens := store.tokenizeUnlocked(context)

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
CharacterNgramCosine returns cosine similarity between character n-gram count
vectors (demo-style fuzzy fallback when co-occurrence rows are sparse).
*/
func CharacterNgramCosine(left string, right string, n int) float64 {
	if n <= 1 {
		n = 2
	}

	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)

	if left == "" || right == "" {
		return 0
	}

	leftRunes := []rune("^" + left + "$")
	rightRunes := []rune("^" + right + "$")

	if len(leftRunes) < n || len(rightRunes) < n {
		return 0
	}

	leftCounts := make(map[string]float64)
	rightCounts := make(map[string]float64)

	for offset := 0; offset <= len(leftRunes)-n; offset++ {
		leftCounts[string(leftRunes[offset:offset+n])]++
	}

	for offset := 0; offset <= len(rightRunes)-n; offset++ {
		rightCounts[string(rightRunes[offset:offset+n])]++
	}

	dot := 0.0
	leftMag := 0.0
	rightMag := 0.0

	for gram, count := range leftCounts {
		dot += count * rightCounts[gram]
		leftMag += count * count
	}

	for _, count := range rightCounts {
		rightMag += count * count
	}

	if leftMag == 0 || rightMag == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftMag) * math.Sqrt(rightMag))
}

/*
Similarity returns cosine similarity between two co-occurrence rows.
*/
func (store *Store) Similarity(
	leftToken string, rightToken string,
) float64 {
	if store == nil {
		return 0
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.similarityUnlocked(leftToken, rightToken)
}

/*
similarityUnlocked implements Similarity; caller must hold store.mu.
*/
func (store *Store) similarityUnlocked(leftToken string, rightToken string) float64 {
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
	if store == nil {
		return levenshteinTokenDistance(left, right)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return levenshteinTokenDistance(left, right)
}

func levenshteinTokenDistance(left string, right string) int {
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
editDistanceUnlocked is an alias used on hot paths where store.mu is already held.
*/
func (store *Store) editDistanceUnlocked(left string, right string) int {
	return levenshteinTokenDistance(left, right)
}

/*
SemanticEquivalent maps a token onto the nearest known vocabulary item using
edit distance first and co-occurrence similarity second.
*/
func (store *Store) SemanticEquivalent(word string) SemanticMatch {
	if store == nil {
		return SemanticMatch{Original: word, Mapped: word, Similarity: 0}
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.semanticEquivalentBody(word)
}

/*
semanticEquivalentBody implements SemanticEquivalent; caller must hold store.mu.
*/
func (store *Store) semanticEquivalentBody(word string) SemanticMatch {
	if _, exists := store.vocabulary[word]; exists {
		return SemanticMatch{
			Original:   word,
			Mapped:     word,
			Similarity: 1,
		}
	}

	bestWord := word
	bestSimilarity := -1.0
	bestNgramSimilarity := -1.0
	ngramBestWord := word
	editMapped := ""
	editDistBest := int(^uint(0) >> 1)

	for _, knownWord := range store.vocabularyOrder {
		if absInt(utf8.RuneCountInString(knownWord)-utf8.RuneCountInString(word)) <= 2 {
			distance := store.editDistanceUnlocked(word, knownWord)
			if distance <= defaultEditDistance {
				if distance < editDistBest {
					editDistBest = distance
					editMapped = knownWord
				}

				continue
			}
		}

		similarity := store.similarityUnlocked(word, knownWord)
		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestWord = knownWord
		}

		ngramSim := CharacterNgramCosine(word, knownWord, 2)
		if ngramSim > bestNgramSimilarity {
			bestNgramSimilarity = ngramSim
			ngramBestWord = knownWord
		}
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

	const ngramConfidenceFloor = 0.35

	if bestNgramSimilarity >= ngramConfidenceFloor {
		return SemanticMatch{
			Original:   word,
			Mapped:     ngramBestWord,
			Similarity: bestNgramSimilarity,
		}
	}

	return SemanticMatch{
		Original:   word,
		Mapped:     word,
		Similarity: 0,
	}
}

/*
AttentionContext returns semantic mappings for content tokens in the context.
*/
func (store *Store) AttentionContext(context string) []SemanticMatch {
	if store == nil {
		return nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	tokens := store.contentTokens(store.tokenizeUnlocked(context))
	matches := make([]SemanticMatch, 0, len(tokens))

	for _, token := range tokens {
		matches = append(matches, store.semanticEquivalentBody(token))
	}

	return matches
}

/*
InterpolatedProbabilities returns weighted next-token probabilities from all
suffixes up to the configured interpolation depth.
*/
func (store *Store) InterpolatedProbabilities(context string, label string) map[string]float64 {
	if store == nil {
		return nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.interpolatedProbabilities(store.tokenizeUnlocked(context), label)
}

/*
SurprisalSeries returns the surprisal for each token in the context.
*/
func (store *Store) SurprisalSeries(context string) []Surprisal {
	if store == nil {
		return nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.surprisalSeriesBody(context)
}

/*
surprisalSeriesBody implements SurprisalSeries; caller must hold store.mu.
*/
func (store *Store) surprisalSeriesBody(context string) []Surprisal {
	tokens := store.tokenizeUnlocked(context)
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
