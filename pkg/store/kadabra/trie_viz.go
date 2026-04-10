package kadabra

import (
	"encoding/json"
	"time"

	"github.com/theapemachine/six/pkg/store/markovtrie"
	"github.com/theapemachine/six/pkg/viz"
)

const trieGraphSnapshotMaxNodes = 800

const trieGraphSnapshotMinInterval = 50 * time.Millisecond

type trieGraphWire struct {
	RootVid   int                       `json:"root_vid"`
	StoreID   uint64                    `json:"store_id"`
	Nodes     []markovtrie.VizGraphNode `json:"nodes"`
	Edges     []markovtrie.VizGraphEdge `json:"edges"`
	Truncated bool                      `json:"truncated"`
}

/*
trieIndex returns the column index for store, or -1 if store is not local.
*/
func (node *Node) trieIndex(store *markovtrie.Store) int {
	if node == nil || store == nil {
		return -1
	}

	tries := node.triesSnapshot()

	for idx := range tries {
		if tries[idx] == store {
			return idx
		}
	}

	return -1
}

/*
publishTrieGraphViz emits a bounded JSON snapshot of the Markov trie graph.
Events are throttled per (node, trie column) so inserts do not flood WS clients.
*/
func (node *Node) publishTrieGraphViz(store *markovtrie.Store) {
	if node == nil || store == nil || !viz.DefaultBus.IsActive() {
		return
	}

	idx := node.trieIndex(store)

	if idx < 0 {
		return
	}

	node.trieGraphVizMu.Lock()

	if node.trieGraphVizLast == nil {
		node.trieGraphVizLast = make(map[int]time.Time)
	}

	now := time.Now()

	if last, ok := node.trieGraphVizLast[idx]; ok && now.Sub(last) < trieGraphSnapshotMinInterval {
		node.trieGraphVizMu.Unlock()

		return
	}

	node.trieGraphVizLast[idx] = now
	node.trieGraphVizMu.Unlock()

	rootVid, graphNodes, graphEdges, truncated := store.SnapshotVizGraph(trieGraphSnapshotMaxNodes)

	wire := trieGraphWire{
		RootVid:   rootVid,
		StoreID:   store.ID,
		Nodes:     graphNodes,
		Edges:     graphEdges,
		Truncated: truncated,
	}

	raw, err := json.Marshal(wire)

	if err != nil {
		return
	}

	viz.DefaultBus.Publish(viz.TrieGraphSnapshotEvent(node.ID, idx, raw))
}
