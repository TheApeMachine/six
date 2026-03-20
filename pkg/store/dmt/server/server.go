package server

import (
	"context"
	"encoding/binary"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/dmt"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/validate"
)

var morton = data.NewMortonCoder()

/*
ForestServer implements the Cap'n Proto RPC interface for the spatial
index. It delegates all storage to a dmt.Forest, which provides persistence
(WAL), distribution (Merkle sync), and read routing (fastest tree).

Keys are Morton-packed uint64 values, stored as 8-byte big-endian keys
in the radix tree to preserve sort order for prefix queries.
*/
type ForestServer struct {
	state       *errnie.State
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Server
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	forest      *dmt.Forest
	workerPool  *pool.Pool
}

type serverOpts func(*ForestServer)

/*
NewForestServer creates a new ForestServer backed by a dmt.Forest.
*/
func NewForestServer(opts ...serverOpts) *ForestServer {
	idx := &ForestServer{
		state:       errnie.NewState("dmt/forest"),
		clientConns: map[string]*rpc.Conn{},
	}

	for _, opt := range opts {
		opt(idx)
	}

	validate.Require(map[string]any{
		"ctx": idx.ctx,
	})

	if idx.forest == nil {
		forest := errnie.Guard(idx.state, func() (*dmt.Forest, error) {
			return dmt.NewForest(dmt.ForestConfig{
				Pool: idx.workerPool,
			})
		})

		idx.forest = forest
	}

	idx.serverSide, idx.clientSide = net.Pipe()
	idx.client = Server_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		idx.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx
}

/*
Client returns a Cap'n Proto client connected to this ForestServer.
*/
func (idx *ForestServer) Client(clientID string) capnp.Client {
	idx.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		idx.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return capnp.Client(idx.client)
}

/*
Close shuts down the RPC connections, underlying net.Pipe, and the forest.
*/
func (idx *ForestServer) Close() error {
	if idx.serverConn != nil {
		errnie.GuardVoid(idx.state, idx.serverConn.Close)
		idx.serverConn = nil
	}

	for clientID, conn := range idx.clientConns {
		if conn != nil {
			errnie.GuardVoid(idx.state, conn.Close)
		}

		delete(idx.clientConns, clientID)
	}

	if idx.serverSide != nil {
		errnie.GuardVoid(idx.state, idx.serverSide.Close)
		idx.serverSide = nil
	}

	if idx.clientSide != nil {
		errnie.GuardVoid(idx.state, idx.clientSide.Close)
		idx.clientSide = nil
	}

	if idx.cancel != nil {
		idx.cancel()
	}

	if idx.forest != nil {
		return idx.forest.Close()
	}

	return nil
}

/*
Done implements the Forest RPC done method.
*/
func (idx *ForestServer) Done(ctx context.Context, call Server_done) error {
	return nil
}

/*
Write stores a Morton-packed key in the forest. The key is encoded as
8-byte big-endian to preserve radix tree sort order.
*/
func (idx *ForestServer) Write(
	ctx context.Context, call Server_write,
) error {
	key := call.Args().Key()
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, key)

	idx.forest.Insert(keyBytes, nil)

	return nil
}

/*
Forest returns the underlying dmt.Forest for direct access by
components that need the raw store (e.g. sequence storage).
*/
func (idx *ForestServer) Forest() *dmt.Forest {
	return idx.forest
}

/*
WithContext sets the context for the server.
*/
func WithContext(ctx context.Context) serverOpts {
	return func(idx *ForestServer) {
		idx.ctx, idx.cancel = context.WithCancel(ctx)
	}
}

/*
WithForest injects a pre-created dmt.Forest.
*/
func WithForest(forest *dmt.Forest) serverOpts {
	return func(idx *ForestServer) {
		idx.forest = forest
	}
}

/*
WithWorkerPool injects the shared worker pool for the backing forest.
*/
func WithWorkerPool(workerPool *pool.Pool) serverOpts {
	return func(idx *ForestServer) {
		idx.workerPool = workerPool
	}
}

/*
SpatialIndexError is a typed error for SpatialIndex failures.
*/
type SpatialIndexError string

const (
	ErrForestInit SpatialIndexError = "spatial-index: forest init failed"
)

/*
Error implements the error interface for SpatialIndexError.
*/
func (err SpatialIndexError) Error() string {
	return string(err)
}
