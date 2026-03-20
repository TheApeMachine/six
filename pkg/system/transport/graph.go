package transport

import (
	"errors"
	"fmt"
	"io"
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
	if graph.registry == nil {
		return 0, errors.New("registry not set")
	}

	if !graph.processed {
		// Process through all edges in the graph
		for from, targets := range graph.edges {
			if node, ok := graph.nodes[from]; ok {
				// Copy from source node to all target nodes
				for _, to := range targets {
					if targetNode, ok := graph.nodes[to]; ok {
						if _, err = io.Copy(targetNode.Component, node.Component); err != nil {
							return 0, fmt.Errorf("graph.Read: %w", err)
						}
					}
				}
			}
		}

		graph.processed = true
	}

	// Read from registry after processing
	return graph.registry.Read(p)
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
	if graph.registry == nil {
		return 0, errors.New("registry not set")
	}

	// Reset processed state when new data comes in
	graph.processed = false

	// Write to registry first
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
	if graph.registry == nil {
		return errors.New("registry not set")
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
	if edges, ok := graph.edges[nodeID]; ok {
		return edges
	}
	return []string{}
}
