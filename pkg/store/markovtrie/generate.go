package markovtrie

import (
	"github.com/theapemachine/six/pkg/core/algo"
)

/*
Generate samples a continuation with beam search; trie walks and ranking stay
in this package, beam supplies only data shapes and pure steps.
*/
func (store *Store) Generate(
	context string, label string, temperature float64, maxLength int,
) (string, error) {
	if store == nil {
		return "", nil
	}

	prediction := algo.NewPrediction()

	store.Walk(store.root, func(node *Node) {
		prediction.Context = append(
			prediction.Context, node.value,
		)
	})

	return prediction.String(), nil
}

/*
Walk performs a depth-first traversal of the trie from the given node,
calling the visitor for each node encountered.
*/
func (store *Store) Walk(node *Node, visitor func(node *Node)) {
	if node == nil {
		return
	}

	visitor(node)

	children := node.Children()

	for _, child := range children {
		store.Walk(child, visitor)
	}
}

/*
WalkPath follows a specific token sequence through the trie,
calling the visitor for each node on the path. Returns the
deepest node reached.
*/
func (store *Store) WalkPath(tokens []string, visitor func(node *Node)) *Node {
	if store.root == nil || len(tokens) == 0 {
		return store.root
	}

	current := store.root
	visitor(current)

	for _, token := range tokens {
		child := current.Child(token)

		if child == nil {
			return current
		}

		current = child
		visitor(current)
	}

	return current
}
