package markovtrie

import (
	"fmt"
	"math"
)

/*
TrieNode stores one token in the markovtrie
and tracks decayed class counts.
*/
type Node struct {
	ID             string
	Token          string
	Children       map[string]*Node
	ClassCounts    map[string]float64
	TotalVisits    float64
	Depth          int
	LastUpdateStep int
}

/*
DeepestNodeID walks exact post-tokenization symbols through the trie including
the configured end token and returns the last reachable node identifier.
*/
func (store *Store) DeepestNodeID(sequence string) string {
	tokens := store.tokensWithEnd(sequence)
	node := store.root

	for _, token := range tokens {
		child := node.Children[token]
		if child == nil {
			return node.ID
		}

		node = child
	}

	return node.ID
}

func (store *Store) touchNode(node *Node, label string, learningRate float64) {
	stepDelta := store.currentStep - node.LastUpdateStep
	if stepDelta > 0 {
		decay := math.Pow(store.decayFactor, float64(stepDelta))
		for knownLabel, count := range node.ClassCounts {
			node.ClassCounts[knownLabel] = count * decay
		}

		node.TotalVisits *= decay
		node.LastUpdateStep = store.currentStep
	}

	node.ClassCounts[label] += learningRate
	node.TotalVisits += learningRate
}

func (store *Store) prune() {
	if store.pruneInterval <= 0 || store.currentStep%store.pruneInterval != 0 {
		return
	}

	store.applyPrune()
}

func (store *Store) applyPrune() {
	threshold := defaultPruneMinimumCount
	if store.adaptive != nil {
		store.adaptive.observeNodeGrowth(store.nodeCount, store.currentStep)
		threshold = store.adaptive.adaptivePruneThreshold(threshold)
	}

	var pruneNode func(node *Node)
	pruneNode = func(node *Node) {
		for _, token := range sortedChildTokens(node) {
			child := node.Children[token]
			if store.EffectiveCount(child, "") < threshold {
				store.nodeCount -= uint64(subtreeSize(child))
				delete(node.Children, token)
				continue
			}

			pruneNode(child)
		}
	}

	pruneNode(store.root)
}

func (store *Store) updateVocabulary(words []string) {
	for _, word := range words {
		if _, exists := store.vocabulary[word]; exists {
			continue
		}

		store.vocabulary[word] = struct{}{}
		store.vocabularyOrder = append(store.vocabularyOrder, word)
	}
}

func (store *Store) updateCoOccurrence(words []string) {
	for wordIndex, leftWord := range words {
		if store.coOccurrence[leftWord] == nil {
			store.coOccurrence[leftWord] = make(map[string]float64)
		}

		startIndex := max(0, wordIndex-defaultCoOccurrenceWindow)
		endIndex := min(len(words)-1, wordIndex+defaultCoOccurrenceWindow)

		for neighborIndex := startIndex; neighborIndex <= endIndex; neighborIndex++ {
			if neighborIndex == wordIndex {
				continue
			}

			rightWord := words[neighborIndex]
			store.coOccurrence[leftWord][rightWord]++
		}
	}
}

func (store *Store) tokensWithEnd(sequence string) []string {
	tokens := append([]string(nil), store.Tokenize(sequence)...)
	return append(tokens, store.endToken)
}

func (store *Store) contentTokens(tokens []string) []string {
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == store.endToken || isSeparatorToken(token) {
			continue
		}

		words = append(words, token)
	}

	return words
}

func (store *Store) addLabel(label string) {
	if _, exists := store.labelSet[label]; exists {
		return
	}

	store.labelSet[label] = struct{}{}
	store.labels = append(store.labels, label)
}

func (store *Store) nextConceptLabel() string {
	label := fmt.Sprintf("%s%d", defaultConceptLabelPrefix, store.conceptCounter)
	store.conceptCounter++

	return label
}

func (store *Store) bestLabelScore(scores map[string]float64) (string, float64) {
	bestLabel := ""
	maxScore := -1.0

	for label, score := range scores {
		if score > maxScore {
			maxScore = score
			bestLabel = label
		}
	}

	return bestLabel, maxScore
}

/*
EpisodicBufferSnapshot returns a copy of the episodic ring buffer for UIs and
telemetry (IDs mirror enqueue order).
*/
func (store *Store) EpisodicBufferSnapshot() []EpisodicEpisode {
	if store == nil || len(store.episodicBuffer) == 0 {
		return nil
	}

	out := make([]EpisodicEpisode, 0, len(store.episodicBuffer))

	for _, event := range store.episodicBuffer {
		out = append(out, EpisodicEpisode{
			ID:        event.ID,
			Tokens:    append([]string(nil), store.contentTokens(event.Tokens)...),
			Label:     event.Label,
			Timestamp: event.Step,
		})
	}

	return out
}

func (store *Store) pushEpisodic(tokens []string, label string) {
	if store.episodicCapacity <= 0 {
		return
	}

	store.episodicSequenceCounter++
	event := episodicEvent{
		ID:     fmt.Sprintf("ep_%d", store.episodicSequenceCounter),
		Tokens: append([]string(nil), tokens...),
		Label:  label,
		Step:   store.currentStep,
	}

	store.episodicBuffer = append(store.episodicBuffer, event)
	overflow := len(store.episodicBuffer) - store.episodicCapacity
	if overflow > 0 {
		store.episodicBuffer = store.episodicBuffer[overflow:]
	}
}

func (store *Store) blendEpisodicTail(contextTokens []string, label string, trie map[string]float64) map[string]float64 {
	if store.episodicCapacity <= 0 || len(store.episodicBuffer) == 0 {
		return trie
	}

	episodic := store.episodicNextDistribution(contextTokens, label)
	if len(episodic) == 0 {
		return trie
	}

	alpha := store.episodicAlpha
	if store.adaptive != nil {
		alpha = store.adaptive.adaptiveEpisodicBlend(alpha)

		// Track episodic quality: how much mass did episodic contribute?
		episodicMass := 0.0
		trieMass := 0.0
		for _, v := range episodic {
			episodicMass += v
		}
		for _, v := range trie {
			trieMass += v
		}
		store.adaptive.observeEpisodicQuality(episodicMass, trieMass)
	}

	if alpha <= 0 {
		return trie
	}

	if len(trie) == 0 {
		return episodic
	}

	merged := make(map[string]float64)

	for token, probability := range trie {
		merged[token] = (1 - alpha) * probability
	}

	for token, probability := range episodic {
		merged[token] += alpha * probability
	}

	normalizeProbabilityMap(merged)

	return merged
}

func normalizeProbabilityMap(values map[string]float64) {
	total := 0.0

	for _, probability := range values {
		total += probability
	}

	if total == 0 {
		return
	}

	for token := range values {
		values[token] /= total
	}
}

func (store *Store) episodicNextDistribution(contextTokens []string, label string) map[string]float64 {
	limit := store.episodicNeighborLimit
	if limit <= 0 {
		limit = defaultEpisodicNeighborLimit
	}

	counts := make(map[string]float64)
	matches := 0
	bufferLength := len(store.episodicBuffer)

	for index := len(store.episodicBuffer) - 1; index >= 0 && matches < limit; index-- {
		event := store.episodicBuffer[index]
		if label != "" && event.Label != label {
			continue
		}

		nextToken, found := nextTokenAfterContext(event.Tokens, contextTokens)
		if !found {
			continue
		}

		recency := 1.0
		gamma := store.episodicDecayGamma

		if gamma > 0 && gamma < 1 {
			recency = math.Pow(gamma, float64(matches))
		} else {
			weight := store.episodicRecencyWeight
			if weight > 0 && bufferLength > 0 {
				recency += float64(index) / float64(bufferLength) * weight
			}
		}

		counts[nextToken] += recency
		matches++
	}

	normalizeProbabilityMap(counts)

	return counts
}

func nextTokenAfterContext(sequence []string, context []string) (string, bool) {
	if len(sequence) == 0 {
		return "", false
	}

	if len(context) == 0 {
		return sequence[0], true
	}

	for start := 0; start <= len(sequence)-len(context)-1; start++ {
		matched := true

		for contextIndex := range context {
			if sequence[start+contextIndex] != context[contextIndex] {
				matched = false

				break
			}
		}

		if !matched {
			continue
		}

		nextIndex := start + len(context)
		if nextIndex >= len(sequence) {
			return "", false
		}

		return sequence[nextIndex], true
	}

	return "", false
}
