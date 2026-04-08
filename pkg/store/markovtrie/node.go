package markovtrie

import (
	"maps"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
childMap is an immutable token→Node map swapped atomically so trie
branching is lock-free. Copy-on-write: mutators clone, insert, CAS.
*/
type childMap struct {
	m map[string]*Node
}

/*
Node stores one token in the MarkovTrie
and tracks decayed class counts. Children are keyed by the token
string of the edge value, giving the trie its branching structure.
*/
type Node struct {
	ID          uint64
	value       primitive.Value
	children    atomic.Pointer[childMap]
	TotalVisits atomic.Uint64
	Depth       int
	parent      atomic.Pointer[Node]
}

func NewNode(value primitive.Value) *Node {
	node := &Node{
		ID:    value.ID(),
		value: value,
	}

	node.children.Store(&childMap{m: make(map[string]*Node)})

	return node
}

/*
Child returns the child for the given token, or nil if absent.
*/
func (node *Node) Child(token string) *Node {
	snap := node.children.Load()

	if snap == nil {
		return nil
	}

	return snap.m[token]
}

/*
Children returns a snapshot of all child nodes.
*/
func (node *Node) Children() map[string]*Node {
	snap := node.children.Load()

	if snap == nil {
		return nil
	}

	return snap.m
}

/*
storeChild publishes a new child under the edge keyed by value's
token using copy-on-write CAS. If a child already exists for
that token, it is replaced.
*/
func (node *Node) storeChild(value primitive.Value, child *Node) {
	if child == nil {
		return
	}

	token := trieEdgeKey(value)
	child.Depth = node.Depth + 1

	for {
		old := node.children.Load()

		var base map[string]*Node

		if old != nil && old.m != nil {
			base = make(map[string]*Node, len(old.m)+1)

			maps.Copy(base, old.m)
		} else {
			base = make(map[string]*Node, 1)
		}

		base[token] = child
		child.parent.Store(node)
		next := &childMap{m: base}

		if node.children.CompareAndSwap(old, next) {
			return
		}
	}
}
