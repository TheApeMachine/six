package markovtrie

import (
	"sort"
)

/*
sortedChildTokens returns child edge labels in deterministic order for pruning,
pattern extraction, and beam expansion tie-breaking.
*/
func sortedChildTokens(node *Node) []string {
	if node == nil {
		return nil
	}

	tokens := make([]string, 0, len(node.Children))
	for token := range node.Children {
		tokens = append(tokens, token)
	}

	sort.Strings(tokens)

	return tokens
}

/*
subtreeSize counts nodes in a rooted subtree including the root, for prune accounting.
*/
func subtreeSize(node *Node) int {
	if node == nil {
		return 0
	}

	count := 0
	stack := []*Node{node}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++

		for _, child := range current.Children {
			if child != nil {
				stack = append(stack, child)
			}
		}
	}

	return count
}
