package remote

import (
	"context"
	"io"
	"sync"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/cluster"
)

/*
Router manages a set of remote Nodes and implements kernel.Backend
(io.ReadWriteCloser + Available). It bridges the cluster.Router
capability model with the kernel dispatch layer, routing writes to the
best available remote dmt peer.

Peers are added via AddPeer. The cluster.Router reference is kept for
future integration with dynamic capability discovery.
*/
type Router struct {
	state   *errnie.State
	ctx     context.Context
	cancel  context.CancelFunc
	cluster *cluster.Router
	nodes   []*Node
	active  *Node
	mu      sync.RWMutex
}

type routerOpts func(*Router)

/*
NewRouter creates a remote router. Without peers it reports Available 0.
*/
func NewRouter(opts ...routerOpts) *Router {
	router := &Router{
		state: errnie.NewState("kernel/remote/router"),
		nodes: make([]*Node, 0, 8),
	}

	for _, opt := range opts {
		opt(router)
	}

	return router
}

/*
Write routes the payload to the best available Node.
*/
func (router *Router) Write(p []byte) (n int, err error) {
	node := router.best()

	if node == nil {
		return 0, io.ErrClosedPipe
	}

	return node.Write(p)
}

/*
Read reads from the currently active Node.
*/
func (router *Router) Read(p []byte) (n int, err error) {
	node := router.best()

	if node == nil {
		return 0, io.EOF
	}

	return node.Read(p)
}

/*
Close tears down every remote Node connection.
*/
func (router *Router) Close() error {
	router.mu.Lock()
	defer router.mu.Unlock()

	var closeErr error

	for _, node := range router.nodes {
		if err := node.Close(); err != nil {
			closeErr = err
		}
	}

	router.nodes = router.nodes[:0]
	router.active = nil

	if router.cancel != nil {
		router.cancel()
	}

	return closeErr
}

/*
Available returns the count of reachable remote Nodes.
*/
func (router *Router) Available() (int, error) {
	router.mu.RLock()
	defer router.mu.RUnlock()

	total := 0

	for _, node := range router.nodes {
		n, _ := node.Available()
		total += n
	}

	return total, nil
}

/*
AddPeer dials a remote dmt NetworkNode at addr, creates a Node, and
registers it. Returns an error if the initial connection fails.
*/
func (router *Router) AddPeer(addr string) error {
	node := NewNode(
		NodeWithContext(router.ctx),
		NodeWithAddress(addr),
	)

	router.mu.Lock()
	router.nodes = append(router.nodes, node)
	router.mu.Unlock()

	return nil
}

/*
Nodes returns a snapshot of the current node list for inspection.
*/
func (router *Router) Nodes() []*Node {
	router.mu.RLock()
	defer router.mu.RUnlock()

	out := make([]*Node, len(router.nodes))
	copy(out, router.nodes)

	return out
}

/*
best returns the first available Node, caching the selection in active.
Falls back to the first Node regardless of availability.
*/
func (router *Router) best() *Node {
	router.mu.RLock()
	defer router.mu.RUnlock()

	if router.active != nil {
		n, _ := router.active.Available()

		if n > 0 {
			return router.active
		}
	}

	for _, node := range router.nodes {
		n, _ := node.Available()

		if n > 0 {
			router.active = node
			return node
		}
	}

	if len(router.nodes) > 0 {
		router.active = router.nodes[0]
		return router.active
	}

	return nil
}

/*
RouterWithContext sets a cancellable context.
*/
func RouterWithContext(ctx context.Context) routerOpts {
	return func(router *Router) {
		router.ctx, router.cancel = context.WithCancel(ctx)
	}
}

/*
RouterWithCluster injects the cluster-level capability router for
local service discovery.
*/
func RouterWithCluster(clusterRouter *cluster.Router) routerOpts {
	return func(router *Router) {
		router.cluster = clusterRouter
	}
}

/*
RouterError is a typed error for Router failures.
*/
type RouterError string

const (
	ErrNoPeers RouterError = "router: no peers available"
)

/*
Error implements the error interface.
*/
func (routerErr RouterError) Error() string {
	return string(routerErr)
}
