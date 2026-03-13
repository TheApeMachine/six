package substrate

import (
	"fmt"

	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
)

// ASTNode represents a node in the graph's abstract syntax tree. It holds the node's topological Level, its chord Label, orientational drift Theta, and references to its Children and Leaves for tree organization.
type ASTNode struct {
	Level    int
	Label    data.Chord
	Theta    float64
	Children []*ASTNode
	Leaves   [][]data.Chord
}

// ASTNode.Print prints the node's Level, Label active bit count, Theta in radians, and recursively prints children with the provided indent string.
func (node *ASTNode) Print(indent string) {
	fmt.Printf("%sLevel %d: Label %d bits, Theta: %.2f radians\n", indent, node.Level, node.Label.ActiveCount(), node.Theta)
	for _, child := range node.Children {
		child.Print(indent + "  ")
	}
}

func extractSharedInvariant(sequences [][]data.Chord) data.Chord {
	if len(sequences) == 0 {
		return data.Chord{}
	}

	initialized := false
	var invariant data.Chord

	for _, seq := range sequences {
		var seqUnion data.Chord
		seqInit := false

		for _, chord := range seq {
			if chord.ActiveCount() == 0 {
				continue
			}

			if !seqInit {
				seqUnion = chord
				seqInit = true
			} else {
				seqUnion = seqUnion.OR(chord)
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
		return data.Chord{}
	}

	return invariant
}

func xorSequence(seq []data.Chord, label data.Chord) []data.Chord {
	var out []data.Chord
	for _, chord := range seq {
		residue := chord.XOR(label)
		if residue.ActiveCount() > 0 {
			console.Trace("xorSequence", "residue", residue)
			out = append(out, residue)
		}
	}

	console.Trace("xorSequence", "out", out)
	return out
}
