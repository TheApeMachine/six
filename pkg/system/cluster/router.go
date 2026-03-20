package cluster

import (
	"context"
	"fmt"

	capnp "capnproto.org/go/capnp/v3"
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
}

/*
Router holds registered services and selects the best available one
when a capability is requested. It embeds transport.Stream so it
participates in io pipelines like everything else.
*/
type Router struct {
	*transport.Stream
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
	router.services[capability] = append(router.services[capability], svc)
}

/*
Resolve returns the first service registered for the capability, or nil.
*/
func (router *Router) Resolve(capability string) Service {
	svcs := router.services[capability]

	if len(svcs) == 0 {
		return nil
	}

	return svcs[0]
}

/*
Get resolves a capability and returns a Cap'n Proto client for the given
clientID. Returns an error if no service is registered under that name.
*/
func (router *Router) Get(_ context.Context, capability string, clientID string) (capnp.Client, error) {
	svc := router.Resolve(capability)
	if svc == nil {
		return capnp.Client{}, fmt.Errorf("cluster: no service registered for %q", capability)
	}

	return svc.Client(clientID), nil
}

/*
RouterWithContext sets a cancellable context.
*/
func RouterWithContext(ctx context.Context) routerOpts {
	return func(router *Router) {
		router.ctx, router.cancel = context.WithCancel(ctx)
	}
}
