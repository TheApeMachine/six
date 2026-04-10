package markovtrie

import "slices"

/*
VizGraphNode is one vertex in the live Markov trie graph as seen by viz.

Vid is a stable snapshot-local index (BFS order). ValueID is primitive.Value’s id
and may repeat across distinct vertices when the same token payload is stored
on multiple structural nodes — Vid always distinguishes them.
*/
type VizGraphNode struct {
	Vid     int    `json:"vid"`
	ValueID uint64 `json:"value_id"`
	Depth   int    `json:"depth"`
	Visits  uint64 `json:"visits"`
	Token   string `json:"token"`
}

/*
VizGraphEdge is one directed token-labeled edge (parent Vid → child Vid).
*/
type VizGraphEdge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Token string `json:"token"`
}

/*
SnapshotVizGraph walks the trie in breadth-first order up to maxNodes vertices.
Order and edge enumeration are sorted by token string so replay is deterministic.
Vertices are indexed 0..n-1 by BFS (Vid); edges reference those indices.
When the frontier is non-empty after the cap applies, Truncated is true.
*/
func (store *Store) SnapshotVizGraph(maxNodes int) (rootVid int, nodes []VizGraphNode, edges []VizGraphEdge, truncated bool) {
	if store == nil || store.root == nil || maxNodes < 1 {
		return 0, nil, nil, false
	}

	queue := []*Node{store.root}
	seen := make(map[*Node]struct{}, maxNodes)
	order := make([]*Node, 0, maxNodes)

	for len(queue) > 0 && len(order) < maxNodes {
		node := queue[0]
		queue = queue[1:]

		if _, dup := seen[node]; dup {
			continue
		}

		seen[node] = struct{}{}
		order = append(order, node)

		if len(order) >= maxNodes {
			break
		}

		childMap := node.Children()

		keys := make([]string, 0, len(childMap))

		for key := range childMap {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			child := childMap[key]

			if child == nil {
				continue
			}

			if _, already := seen[child]; already {
				continue
			}

			queue = append(queue, child)
		}
	}

	truncated = len(queue) > 0

	vidOf := make(map[*Node]int, len(order))

	for idx, node := range order {
		vidOf[node] = idx
	}

	nodes = make([]VizGraphNode, 0, len(order))

	for _, node := range order {
		nodes = append(nodes, VizGraphNode{
			Vid:     vidOf[node],
			ValueID: node.ID,
			Depth:   node.Depth,
			Visits:  node.TotalVisits.Load(),
			Token:   trieEdgeKey(node.value),
		})
	}

	edges = make([]VizGraphEdge, 0, len(order))

	for _, parent := range order {
		childMap := parent.Children()

		keys := make([]string, 0, len(childMap))

		for key := range childMap {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			child := childMap[key]

			if child == nil {
				continue
			}

			childVid, ok := vidOf[child]

			if !ok {
				continue
			}

			parentVid := vidOf[parent]

			edges = append(edges, VizGraphEdge{
				From:  parentVid,
				To:    childVid,
				Token: key,
			})
		}
	}

	return 0, nodes, edges, truncated
}
