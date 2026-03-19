package substrate

import (
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
ASTNode represents a node in the graph's abstract syntax tree built by
RecursiveFold. Each node stores the shared invariant (Label) extracted
via AND across its input sequences, and the Morton keys that produced
those sequences so the projection plane can recover the original bytes.
*/
type ASTNode struct {
	state     *errnie.State
	Level     int
	Bin       int
	Label     primitive.Value
	LabelMeta primitive.Value
	Theta     float64
	Keys      []uint64
	Children  []*ASTNode
	Leaves    [][]primitive.Value
}

/*
Walk descends the AST by cancelling the prompt against each node's Label.
At each level, the prompt is XORed with the node's invariant. The child
whose label has the highest similarity to the residue is followed. Returns
the leaf node and the leaf's Morton keys (the answer bytes).
*/
func (node *ASTNode) Walk(prompt primitive.Value) (matched *ASTNode, leafKeys []uint64) {
	if len(node.Children) == 0 {
		return node, node.Keys
	}

	residue := errnie.Guard(node.state, func() (primitive.Value, error) {
		return prompt.XOR(node.Label)
	})

	bestChild := (*ASTNode)(nil)
	bestSimilarity := -1

	for _, child := range node.Children {
		similarity := residue.Similarity(child.Label)

		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestChild = child
		}
	}

	if bestChild == nil || bestSimilarity == 0 {
		return node, node.Keys
	}

	return bestChild.Walk(residue)
}

/*
Collect gathers all Morton keys reachable from this node downward.
*/
func (node *ASTNode) Collect() []uint64 {
	childResults := make([][]uint64, 0, len(node.Children))
	totalLen := len(node.Keys)

	for _, child := range node.Children {
		chunk := child.Collect()
		childResults = append(childResults, chunk)
		totalLen += len(chunk)
	}

	result := make([]uint64, 0, totalLen)
	result = append(result, node.Keys...)

	for _, chunk := range childResults {
		result = append(result, chunk...)
	}

	return result
}

func extractSharedInvariant(sequences [][]data.Value) data.Value {
	if len(sequences) == 0 {
		return data.Value{}
	}

	initialized := false
	var invariant data.Value

	for _, seq := range sequences {
		var seqUnion data.Value
		seqInit := false

		for _, value := range seq {
			if value.ActiveCount() == 0 {
				continue
			}

			if !seqInit {
				seqUnion = value
				seqInit = true
			} else {
				seqUnion = seqUnion.OR(value)
			}
		}

		if !seqInit {
			continue
		}

		if !initialized {
			invariant = seqUnion
			initialized = true
		} else {
			invariant = invariant.AND(seqUnion)
		}
	}

	if !initialized {
		return data.Value{}
	}

	return invariant
}

/*
xorSequence extracts residue boundaries by applying a logical XOR between
an active context label and a sequence. It returns only the non-zero residues.
*/
func xorSequence(seq []data.Value, label data.Value) []data.Value {
	var out []data.Value

	for _, value := range seq {
		residue := value.XOR(label)

		if residue.ActiveCount() > 0 {
			out = append(out, residue)
		}
	}

	return out
}
