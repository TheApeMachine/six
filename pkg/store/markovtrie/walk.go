package markovtrie

/*
Walk performs a depth-first traversal of the trie from the given node,
calling the visitor for each node encountered.
*/
func (store *Store) Walk(node *Node, visitor func(node *Node)) {
	if node == nil {
		return
	}

	visitor(node)

	children := node.Children()

	for _, child := range children {
		store.Walk(child, visitor)
	}
}

/*
WalkPath follows a specific token sequence through the trie,
calling the visitor for each node on the path. Returns the
deepest node reached.
*/
func (store *Store) WalkPath(tokens []string, visitor func(node *Node)) *Node {
	if store.root == nil || len(tokens) == 0 {
		return store.root
	}

	current := store.root
	visitor(current)

	for _, token := range tokens {
		child := current.Child(token)

		if child == nil {
			return current
		}

		current = child
		visitor(current)
	}

	return current
}
