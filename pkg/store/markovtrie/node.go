package markovtrie

import (
	"maps"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
stringNodeMap is an immutable snapshot of outgoing trie edges. The map
lives behind atomic.Pointer so readers never block writers and writers
retry on CAS failure instead of taking a store-wide mutex.
*/
type stringNodeMap struct {
	m map[string]*Node
}

/*
labelFloatMap is an immutable per-node class histogram snapshot.
*/
type labelFloatMap struct {
	m map[string]float64
}

/*
Node stores one token in the MarkovTrie
and tracks decayed class counts.
*/
type Node struct {
	ID             string
	Value          primitive.Value
	Token          string
	children       atomic.Pointer[stringNodeMap]
	classCounts    atomic.Pointer[labelFloatMap]
	TotalVisits    atomic.Uint64
	Depth          int
	LastUpdateStep atomic.Int32
}

func childMapOf(node *Node) map[string]*Node {
	if node == nil {
		return nil
	}

	snap := node.children.Load()

	if snap == nil {
		return nil
	}

	return snap.m
}

/*
childAt returns the child for an edge label without allocating.
*/
func (node *Node) childAt(token string) *Node {
	m := childMapOf(node)

	if m == nil {
		return nil
	}

	return m[token]
}

/*
storeChild publishes a new child under token using copy-on-write CAS.
*/
func (node *Node) storeChild(token string, child *Node) {
	for {
		old := node.children.Load()
		var newM map[string]*Node

		if old == nil || old.m == nil {
			newM = map[string]*Node{token: child}
		} else {
			newM = maps.Clone(old.m)
			newM[token] = child
		}

		newSnap := &stringNodeMap{m: newM}

		if node.children.CompareAndSwap(old, newSnap) {
			return
		}
	}
}

func labelFloatMapOf(node *Node) map[string]float64 {
	if node == nil {
		return nil
	}

	lm := node.classCounts.Load()

	if lm == nil {
		return nil
	}

	return lm.m
}

/*
DeepestNodeID walks exact post-tokenization symbols through the trie including
the configured end token and returns the last reachable node identifier.
*/
func (store *Store) DeepestNodeID(sequence string) string {
	if store == nil {
		return ""
	}

	tokens := store.bpe.Encode(sequence)
	node := store.root

	for _, token := range tokens {
		child := node.childAt(token)

		if child == nil {
			return node.ID
		}

		node = child
	}

	return node.ID
}
