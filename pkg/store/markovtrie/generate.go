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
Walk walks the trie from the given node, calling
the visitor function for each node.
*/
func (store *Store) Walk(node *Node, visitor func(node *Node)) {
	if node == nil {
		return
	}

	visitor(node)
	store.Walk(*node.children.Load(), visitor)
}
