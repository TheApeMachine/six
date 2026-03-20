package transport

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

/*
Node represents a vertex in the workflow graph. Each node contains a component
that can process data through the workflow.

Fields:
  - ID: Unique identifier for the node
  - Component: The data processing component implementing io.ReadWriter
*/
type Node struct {
	ID        string
	Component io.ReadWriteCloser
}

/*
Edge represents a directed connection between two nodes in the workflow graph.

Fields:
  - From: ID of the source node
  - To: ID of the destination node
*/
type Edge struct {
	From string
	To   string
}

/*
Graph represents a directed graph of workflow components. It manages the flow of data
between connected components and maintains a registry of processed states.

Fields:
  - registry: Central component for storing processed data
  - nodes: Map of node IDs to Node instances
  - edges: Map of source node IDs to lists of destination node IDs
  - processed: Flag indicating if the current data has been processed through the graph
*/
type Graph struct {
	mu        sync.Mutex
	registry  io.ReadWriteCloser
	nodes     map[string]*Node
	edges     map[string][]string
	processed bool
}

/*
GraphOption is a function type for configuring a Graph instance using the functional
options pattern.
*/
type GraphOption func(*Graph)

/*
NewGraph creates a new Graph instance with the provided options.

Parameters:
  - opts: Variable number of GraphOption functions to configure the graph

Returns:
  - *Graph: A new Graph instance configured with the provided options
*/
func NewGraph(opts ...GraphOption) *Graph {
	graph := &Graph{
		registry: nil,
		nodes:    make(map[string]*Node),
		edges:    make(map[string][]string),
	}

	for _, opt := range opts {
		opt(graph)
	}

	return graph
}

/*
Read implements io.Reader. It processes data through the graph if needed and
reads from the registry.

Parameters:
  - p: Byte slice to read data into

Returns:
  - n: Number of bytes read
  - err: Any error that occurred during reading
*/
func (graph *Graph) Read(p []byte) (n int, err error) {
	graph.mu.Lock()

	if graph.registry == nil {
		graph.mu.Unlock()

		return 0, errors.New("registry not set")
	}

	var edgeSnap map[string][]string
	var nodeSnap map[string]*Node

	if !graph.processed {
		edgeSnap = make(map[string][]string, len(graph.edges))

		for from, targets := range graph.edges {
			edgeSnap[from] = append([]string(nil), targets...)
		}

		nodeSnap = make(map[string]*Node, len(graph.nodes))

		for id, node := range graph.nodes {
			nodeSnap[id] = node
		}

		graph.processed = true
	}

	reg := graph.registry
	graph.mu.Unlock()

	if edgeSnap != nil {
		for from, targets := range edgeSnap {
			node, ok := nodeSnap[from]
			if !ok {
				continue
			}

			for _, to := range targets {
				targetNode, ok := nodeSnap[to]
				if !ok {
					continue
				}

				var copied int64
				var copyErr error

				if copied, copyErr = io.Copy(targetNode.Component, node.Component); copyErr != nil {
					return 0, fmt.Errorf("graph.Read: %w", copyErr)
				}

				if copied == 0 {
					continue
				}
			}
		}
	}

	return reg.Read(p)
}

/*
Write implements io.Writer. It writes data to the registry and marks the graph
as needing processing.

Parameters:
  - p: Byte slice containing data to write

Returns:
  - n: Number of bytes written
  - err: Any error that occurred during writing
*/
func (graph *Graph) Write(p []byte) (n int, err error) {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	if graph.registry == nil {
		return 0, errors.New("registry not set")
	}

	graph.processed = false

	if n, err = graph.registry.Write(p); err != nil {
		return n, fmt.Errorf("graph.Write: %w", err)
	}

	return n, nil
}

/*
Close implements io.Closer. It closes the registry if it exists.

Returns:
  - error: Any error that occurred while closing the registry
*/
func (graph *Graph) Close() error {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	if graph.registry == nil {
		return nil
	}

	return graph.registry.Close()
}

/*
WithRegistry is a GraphOption that sets the registry component for the graph.

Parameters:
  - registry: The io.ReadWriteCloser to use as the graph's registry
*/
func WithRegistry(registry io.ReadWriteCloser) GraphOption {
	return func(graph *Graph) {
		graph.mu.Lock()
		defer graph.mu.Unlock()

		graph.registry = registry
	}
}

/*
WithNode is a GraphOption that adds a node to the graph.

Parameters:
  - node: The Node to add to the graph
*/
func WithNode(node *Node) GraphOption {
	return func(graph *Graph) {
		graph.mu.Lock()
		defer graph.mu.Unlock()

		graph.nodes[node.ID] = node
	}
}

/*
WithEdge is a GraphOption that adds an edge to the graph.

Parameters:
  - edge: The Edge to add to the graph
*/
func WithEdge(edge *Edge) GraphOption {
	return func(graph *Graph) {
		graph.mu.Lock()
		defer graph.mu.Unlock()

		graph.edges[edge.From] = append(graph.edges[edge.From], edge.To)
	}
}

/*
GetEdges returns all destination node IDs for a given source node ID.

Parameters:
  - nodeID: ID of the source node

Returns:
  - []string: Slice of destination node IDs, empty if none exist
*/
func (graph *Graph) GetEdges(nodeID string) []string {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	if edges, ok := graph.edges[nodeID]; ok {
		out := make([]string, len(edges))
		copy(out, edges)

		return out
	}

	return []string{}
}
