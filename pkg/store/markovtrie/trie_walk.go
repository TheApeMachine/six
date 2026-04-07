package markovtrie

import (
	"sort"
)

/*
sortedChildTokens returns child edge labels in deterministic order for pruning,
pattern extraction, and stable tie-breaks.
*/
func sortedChildTokens(node *Node) []string {
	if node == nil {
		return nil
	}

	m := childMapOf(node)

	if m == nil {
		return nil
	}

	tokens := make([]string, 0, len(m))

	for token := range m {
		tokens = append(tokens, token)
	}

	sort.Strings(tokens)

	return tokens
}
