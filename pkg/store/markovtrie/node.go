package markovtrie

import (
	"fmt"
	"maps"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/errnie"
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
labelMap is a copy-on-write label counter snapshot for one trie node.
It keeps classification readout local to the Value that was actually loaded
instead of routing through the deleted host-side algorithm stack.
*/
type labelMap struct {
	m map[string]uint64
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
	labels      atomic.Pointer[labelMap]
	transition  primitive.FrameMultivector
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
	node.labels.Store(&labelMap{m: make(map[string]uint64)})

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
Labels returns a snapshot of labels observed at this node.
*/
func (node *Node) Labels() map[string]uint64 {
	if node == nil {
		return nil
	}

	snap := node.labels.Load()

	if snap == nil {
		return nil
	}

	return snap.m
}

/*
AddLabel records one supervised label on the node using copy-on-write CAS.
*/
func (node *Node) AddLabel(label string) {
	if node == nil || label == "" {
		return
	}

	backoff := time.Nanosecond

	for {
		old := node.labels.Load()

		var base map[string]uint64

		if old != nil && old.m != nil {
			base = make(map[string]uint64, len(old.m)+1)
			maps.Copy(base, old.m)
		} else {
			base = make(map[string]uint64, 1)
		}

		base[label]++
		next := &labelMap{m: base}

		if node.labels.CompareAndSwap(old, next) {
			return
		}

		runtime.Gosched()

		time.Sleep(backoff)

		backoff *= 2

		if backoff > time.Millisecond {
			backoff = time.Millisecond
		}
	}
}

/*
TransitionMotor returns the PGA motor that moves from the parent Value to this
node's Value. The root has no parent, so its motor is zero.
*/
func (node *Node) TransitionMotor() primitive.FrameMultivector {
	if node == nil {
		return primitive.FrameMultivector{}
	}

	return node.transition
}

/*
storeChild publishes a new child under the edge keyed by value's
token using copy-on-write CAS. If a child already exists for
that token, it is replaced.
*/
func (node *Node) storeChild(value primitive.Value, child *Node) (err error) {
	if child == nil {
		return errnie.Error(fmt.Errorf("node.storeChild: nil child"))
	}

	token := trieEdgeKey(value)

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
			return nil
		}
	}
}
