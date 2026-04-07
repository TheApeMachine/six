package markovtrie

import (
	"sync/atomic"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Node stores one token in the MarkovTrie
and tracks decayed class counts.
*/
type Node struct {
	ID          uint64
	value       primitive.Value
	children    atomic.Pointer[*Node]
	TotalVisits atomic.Uint64
	Depth       int
}

func NewNode(value primitive.Value) *Node {
	return &Node{
		ID:       value.ID(),
		value:    value,
		children: atomic.Pointer[*Node]{},
	}
}

/*
storeChild publishes a new child under token using copy-on-write CAS.
*/
func (node *Node) storeChild(value primitive.Value, child *Node) {
	if child == nil {
		return
	}

	newNode := NewNode(value)
	child.children.Store(&newNode)
}
