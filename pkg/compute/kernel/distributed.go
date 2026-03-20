package kernel

import (
	"context"
	"fmt"
	"io"

	"github.com/theapemachine/six/pkg/compute/kernel/remote"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/cluster"
)

/*
DistributedBackend routes io through a set of remote dmt peers via
Cap'n Proto RadixRPC. It implements kernel.Backend so it participates
in the Builder's priority chain alongside CPU, Metal, and CUDA.

All data flows through io.ReadWriteCloser: Write sends keys to remote
peers via RadixRPC.Insert, Read drains sync/response data. The old
custom TCP protocol and UDP broadcast discovery are replaced by the
remote.Router which manages RadixRPC connections to configured peers.
*/
type DistributedBackend struct {
	state  *errnie.State
	ctx    context.Context
	cancel context.CancelFunc
	router *remote.Router
}

type distributedOpts func(*DistributedBackend) error

/*
NewDistributedBackend creates a DistributedBackend backed by a
remote.Router. Without peers the backend reports Available 0 and
the Builder will fall through to a local backend.
*/
func NewDistributedBackend(opts ...distributedOpts) (*DistributedBackend, error) {
	backend := &DistributedBackend{
		state: errnie.NewState("kernel/distributed"),
	}

	for _, opt := range opts {
		if optErr := opt(backend); optErr != nil {
			return nil, optErr
		}
	}

	if backend.router == nil {
		backend.router = remote.NewRouter(
			remote.RouterWithContext(backend.ctx),
		)
	}

	return backend, nil
}

/*
Available reports the number of reachable remote peers.
*/
func (backend *DistributedBackend) Available() (int, error) {
	if backend == nil || backend.router == nil {
		return 0, NewDistributedError(DistributedErrorBackendUnavailable)
	}

	return backend.router.Available()
}

/*
Read drains buffered response data from the active remote peer.
*/
func (backend *DistributedBackend) Read(p []byte) (n int, err error) {
	if backend.router == nil {
		return 0, io.EOF
	}

	return backend.router.Read(p)
}

/*
Write sends raw key bytes to the best available remote peer via
RadixRPC.Insert. Each call is one Insert.
*/
func (backend *DistributedBackend) Write(p []byte) (n int, err error) {
	if backend.router == nil {
		return 0, io.ErrClosedPipe
	}

	return backend.router.Write(p)
}

/*
Close tears down all remote connections.
*/
func (backend *DistributedBackend) Close() error {
	if backend.router != nil {
		return backend.router.Close()
	}

	return nil
}

/*
AddPeer registers a remote dmt peer by address (host:port). The
underlying RadixRPC connection is established lazily on first Write.
*/
func (backend *DistributedBackend) AddPeer(addr string) error {
	if backend.router == nil {
		return fmt.Errorf("distributed: router not initialized")
	}

	return backend.router.AddPeer(addr)
}

/*
Router exposes the underlying remote.Router for direct node management
or inspection by the Booter.
*/
func (backend *DistributedBackend) Router() *remote.Router {
	return backend.router
}

/*
DistributedWithContext sets a cancellable context.
*/
func DistributedWithContext(ctx context.Context) distributedOpts {
	return func(backend *DistributedBackend) error {
		if ctx == nil {
			ctx = context.Background()
		}

		backend.ctx, backend.cancel = context.WithCancel(ctx)

		return nil
	}
}

/*
DistributedWithRouter injects a pre-built remote.Router.
*/
func DistributedWithRouter(router *remote.Router) distributedOpts {
	return func(backend *DistributedBackend) error {
		backend.router = router

		return nil
	}
}

/*
DistributedWithCluster injects the cluster-level capability router
so the remote.Router can discover local services.
*/
func DistributedWithCluster(clusterRouter *cluster.Router) distributedOpts {
	return func(backend *DistributedBackend) error {
		if backend.router == nil {
			backend.router = remote.NewRouter(
				remote.RouterWithContext(backend.ctx),
				remote.RouterWithCluster(clusterRouter),
			)
		}

		return nil
	}
}

/*
DistributedWithPeers connects to the listed peer addresses at creation time.
*/
func DistributedWithPeers(peers []string) distributedOpts {
	return func(backend *DistributedBackend) error {
		if backend.router == nil {
			return fmt.Errorf(
				"distributed: router not initialized; call DistributedWithCluster first",
			)
		}

		for _, addr := range peers {
			errnie.GuardVoid(backend.state, func() error {
				return backend.router.AddPeer(addr)
			})
		}

		return nil
	}
}

/*
StartDistributed is the entry point for distributed networking. It replaces
the old StartDiscovery function. It creates a DistributedBackend wired to
the cluster router, adds configured peers, and returns the backend so the
Booter can register it in the kernel.Builder.
*/
func StartDistributed(
	ctx context.Context,
	clusterRouter *cluster.Router,
	peers []string,
) (*DistributedBackend, error) {
	return NewDistributedBackend(
		DistributedWithContext(ctx),
		DistributedWithCluster(clusterRouter),
		DistributedWithPeers(peers),
	)
}

/*
DistributedErrorType enumerates distributed backend failure modes.
*/
type DistributedErrorType string

const (
	DistributedErrorBackendUnavailable DistributedErrorType = "backend unavailable"
	DistributedErrorNoPeers            DistributedErrorType = "no peers available"
)

/*
DistributedError is a typed error for distributed backend failures.
*/
type DistributedError struct {
	Message string
	Err     DistributedErrorType
}

/*
NewDistributedError creates a DistributedError.
*/
func NewDistributedError(err DistributedErrorType) *DistributedError {
	return &DistributedError{
		Message: string(err),
		Err:     err,
	}
}

/*
Error implements the error interface.
*/
func (err DistributedError) Error() string {
	return fmt.Sprintf("distributed error: %s", err.Err)
}
