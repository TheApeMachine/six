package substrate

import (
	"github.com/theapemachine/six/pkg/store/data"
)

/*
ASTNode represents a node in the graph's abstract
syntax tree. It holds the node's topological Level,
its value Label, and references to its Children and
Leaves for tree organization.
*/
type ASTNode struct {
	Level    int
	Label    data.Value
	Theta    float64
	Children []*ASTNode
	Leaves   [][]data.Value
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
