package sequencer

/*
NodeArena pre-allocates Node storage in contiguous slabs to eliminate
per-node heap allocation. Nodes are referenced by int32 index into the
arena rather than pointers, reducing GC pressure from millions of rapid
Sequitur inserts.
*/
type NodeArena struct {
	nodes []Node
	free  []int32
}

/*
NewNodeArena creates an arena pre-sized for initialCapacity nodes.
*/
func NewNodeArena(initialCapacity int) *NodeArena {
	return &NodeArena{
		nodes: make([]Node, 0, initialCapacity),
	}
}

/*
Alloc returns the index of a fresh or recycled Node.
*/
func (arena *NodeArena) Alloc(val Symbol) int32 {
	if len(arena.free) > 0 {
		idx := arena.free[len(arena.free)-1]
		arena.free = arena.free[:len(arena.free)-1]
		arena.nodes[idx] = Node{Val: val, Prev: -1, Next: -1}
		return idx
	}

	idx := int32(len(arena.nodes))
	arena.nodes = append(arena.nodes, Node{Val: val, Prev: -1, Next: -1})
	return idx
}

/*
Free recycles a node index for later reuse.
*/
func (arena *NodeArena) Free(idx int32) {
	arena.nodes[idx] = Node{Val: -1, Prev: -1, Next: -1}
	arena.free = append(arena.free, idx)
}

/*
Get returns a pointer to the node at idx for field access.
Callers must not hold the returned pointer across any Alloc call
because slice growth invalidates prior element addresses.
*/
func (arena *NodeArena) Get(idx int32) *Node {
	return &arena.nodes[idx]
}
