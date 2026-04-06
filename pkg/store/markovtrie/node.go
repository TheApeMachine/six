package markovtrie

import (
	"math"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Node stores one token in the MarkovTrie
and tracks decayed class counts.
*/
type Node struct {
	ID             string
	Value          *primitive.Value
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
	if store == nil {
		return ""
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	tokens := store.bpe.Encode(sequence)
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
