package cluster

import (
	"context"
	"fmt"
	"math"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/transport"
)

const (
	TOKENIZER  = "tokenizer"
	GRAPH      = "graph"
	FOREST     = "forest"
	PROMPTER   = "prompter"
	HAS        = "has"
	PROGRAM    = "program"
	CANTILEVER = "cantilever"
	MACROINDEX = "macroindex"
)

/*
Service is the capability-routing interface. Types that register with
the Router implement Client to vend a Cap'n Proto client, and Close
for teardown. The Router selects the best available service at resolve
time based on capability match and load.
*/
type Service interface {
	Client(string) capnp.Client
	Close() error
	// Load is a non-negative load estimate; Resolve prefers the lowest value.
	Load() int64
}

/*
Router holds registered services and selects the best available one
when a capability is requested. It embeds transport.Stream so it
participates in io pipelines like everything else.
*/
type Router struct {
	*transport.Stream
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	services map[string][]Service
}

type routerOpts func(*Router)

/*
NewRouter creates a cluster router.
*/
func NewRouter(opts ...routerOpts) *Router {
	router := &Router{
		Stream:   transport.NewStream(),
		services: make(map[string][]Service),
	}

	for _, opt := range opts {
		opt(router)
	}

	return router
}

/*
Register adds a service under the given capability name.
*/
func (router *Router) Register(capability string, svc Service) {
	router.mu.Lock()
	defer router.mu.Unlock()

	router.services[capability] = append(router.services[capability], svc)
}

/*
Resolve returns the least-loaded service registered for the capability, or nil.
*/
func (router *Router) Resolve(capability string) Service {
	router.mu.RLock()
	defer router.mu.RUnlock()

	svcs := router.services[capability]

	var best Service
	bestLoad := int64(math.MaxInt64)

	for _, svc := range svcs {
		if svc == nil {
			continue
		}

		load := svc.Load()
		if load < bestLoad {
			best, bestLoad = svc, load
		}
	}

	return best
}

/*
Get resolves a capability and returns a Cap'n Proto client for the given
clientID. Returns an error if no service is registered under that name.
*/
func (router *Router) Get(
	_ context.Context,
	capability string,
	clientID string,
) (capnp.Client, error) {
	svc := router.Resolve(capability)
	if svc == nil {
		return capnp.Client{}, errnie.Error(
			NewRouterError(RouterErrorNoService),
			"capability", capability,
		)
	}

	return svc.Client(clientID), nil
}

/*
RouterWithContext sets a cancellable context. Router.Close invokes cancel
after closing the embedded Stream.
*/
func RouterWithContext(ctx context.Context) routerOpts {
	return func(router *Router) {
		router.ctx, router.cancel = context.WithCancel(ctx)
	}
}

/*
Close closes the embedded transport Stream first, then cancels the router
child context from RouterWithContext so in-flight cluster work observes
cancellation after the stream has signaled EOF.
*/
func (router *Router) Close() error {
	var streamErr error

	if router.Stream != nil {
		streamErr = router.Stream.Close()
	}

	if router.cancel != nil {
		router.cancel()
	}

	return streamErr
}

type RouterErrorType string

const (
	RouterErrorNoService RouterErrorType = "router: no service registered for"
)

type RouterError struct {
	Message string
	Err     RouterErrorType
}

func NewRouterError(err RouterErrorType) *RouterError {
	return &RouterError{Message: string(err), Err: err}
}

func (err RouterError) Error() string {
	return fmt.Sprintf("router error: %s: %s", err.Message, err.Err)
}
