package server

import (
	"context"
	"encoding/binary"
	"net"
	"sync"

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
	serverConn  *rpc.Conn
	clientConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	connMu      sync.Mutex
	forest      *dmt.Forest
	workerPool  *pool.Pool
	writtenMu   sync.Mutex
	writtenKeys []uint64
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

	serverSide, clientSide := net.Pipe()
	capability := Server_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(capability),
	})

	idx.clientConn = rpc.NewConn(rpc.NewStreamTransport(clientSide), nil)

	return idx
}

/*
Client returns a Cap'n Proto client connected to this ForestServer.
Returns the bootstrap capability from the pre-created client connection.
*/
func (idx *ForestServer) Client(clientID string) capnp.Client {
	idx.connMu.Lock()
	defer idx.connMu.Unlock()

	idx.clientConns[clientID] = idx.clientConn
	return idx.clientConn.Bootstrap(idx.ctx)
}

/*
Load approximates concurrent RPC pressure via active client registrations.
*/
func (idx *ForestServer) Load() int64 {
	return int64(len(idx.clientConns))
}

/*
Close shuts down the RPC connections, underlying net.Pipe, and the forest.
*/
func (idx *ForestServer) Close() error {
	if idx.clientConn != nil {
		errnie.GuardVoid(idx.state, idx.clientConn.Close)
		idx.clientConn = nil
	}

	if idx.serverConn != nil {
		errnie.GuardVoid(idx.state, idx.serverConn.Close)
		idx.serverConn = nil
	}

	idx.connMu.Lock()
	for clientID := range idx.clientConns {
		delete(idx.clientConns, clientID)
	}
	idx.connMu.Unlock()

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
	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	idx.writtenMu.Lock()
	keys := idx.writtenKeys
	idx.writtenKeys = nil
	idx.writtenMu.Unlock()

	list, listErr := res.NewKeys(int32(len(keys)))
	if listErr != nil {
		return listErr
	}

	for i, k := range keys {
		list.Set(i, k)
	}

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

	idx.writtenMu.Lock()
	idx.writtenKeys = append(idx.writtenKeys, key)
	idx.writtenMu.Unlock()

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
