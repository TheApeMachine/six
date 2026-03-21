package topology

/*
UnionFind is a weighted union-find with path compression for tracking
connected components (Betti-0) during filtration sweeps. Amortized
O(α(N)) per operation via path halving and union by rank.
*/
type UnionFind struct {
	parent []int32
	rank   []int16
	count  int
}

/*
NewUnionFind allocates a UnionFind pre-sized for capacity elements.
No sets exist until MakeSet is called; capacity only reserves memory.
*/
func NewUnionFind(capacity int) *UnionFind {
	return &UnionFind{
		parent: make([]int32, 0, capacity),
		rank:   make([]int16, 0, capacity),
	}
}

/*
MakeSet creates a new singleton component and returns its ID.
The ID is the current length of the parent slice before appending,
so IDs are dense and monotonically increasing.
*/
func (uf *UnionFind) MakeSet() int32 {
	id := int32(len(uf.parent))

	uf.parent = append(uf.parent, id)
	uf.rank = append(uf.rank, 0)
	uf.count++

	return id
}

/*
Find returns the root representative using path halving. Each node
on the find path is repointed to its grandparent, flattening the
tree in constant extra work per call without full path compression's
second pass.
*/
func (uf *UnionFind) Find(x int32) int32 {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]]
		x = uf.parent[x]
	}

	return x
}

/*
Union merges two components by rank. Returns true when the two
elements were in distinct components (a death event in the persistence
diagram: the younger component's birth interval ends here).
*/
func (uf *UnionFind) Union(x, y int32) bool {
	rootX := uf.Find(x)
	rootY := uf.Find(y)

	if rootX == rootY {
		return false
	}

	switch {
	case uf.rank[rootX] < uf.rank[rootY]:
		uf.parent[rootX] = rootY
	case uf.rank[rootX] > uf.rank[rootY]:
		uf.parent[rootY] = rootX
	default:
		uf.parent[rootY] = rootX
		uf.rank[rootX]++
	}

	uf.count--

	return true
}

/*
Components returns the current number of distinct components (H_0).
*/
func (uf *UnionFind) Components() int {
	return uf.count
}

/*
Connected reports whether x and y share the same root representative.
*/
func (uf *UnionFind) Connected(x, y int32) bool {
	return uf.Find(x) == uf.Find(y)
}

/*
Reset clears all state for reuse without reallocating the backing slices.
*/
func (uf *UnionFind) Reset() {
	uf.parent = uf.parent[:0]
	uf.rank = uf.rank[:0]
	uf.count = 0
}
