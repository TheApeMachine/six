package markovtrie

import (
	"strings"

	"github.com/theapemachine/six/pkg/core/algo"
)

/*
Generate samples a continuation with beam search; trie walks and ranking stay
in this package, beam supplies only data shapes and pure steps.
*/
func (store *Store) Generate(
	context string, label string, temperature float64, maxLength int,
) (string, error) {
	if store == nil || store.root == nil {
		return "", nil
	}

	_ = label

	prediction := algo.NewPrediction()

	tokens := generateContextTokens(context)

	leaf := store.root

	if len(tokens) > 0 {
		leaf = store.WalkPath(tokens, func(node *Node) {
			prediction.Context = append(prediction.Context, node.value)
		})
	}

	if leaf == nil {
		leaf = store.root
	}

	children := leaf.Children()

	if children == nil || len(children) == 0 {
		return "", nil
	}

	temp := temperature

	if temp <= 0 {
		temp = 1
	}

	for _, child := range children {
		visits := float64(child.TotalVisits.Load())
		score := visits / temp

		prediction.Continuations = append(prediction.Continuations, algo.Continuation{
			Sequence: []byte(child.value.String()),
			Score:    score,
			Origin:   store.ID,
		})
	}

	if maxLength > 0 && len(prediction.Continuations) > maxLength {
		prediction.Continuations = prediction.Continuations[:maxLength]
	}

	return prediction.String(), nil
}

func generateContextTokens(context string) []string {
	fields := strings.Fields(context)

	if len(fields) == 0 {
		return nil
	}

	return fields
}
