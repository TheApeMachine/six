package lsm

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/validate"
)

var morton = data.NewMortonCoder()

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Keys are packed via MortonCoder.Pack(localDepth, symbol).
Data values are deterministic (one per byte value). Meta values
accumulate as a list per key from different topological contexts.
*/
type SpatialIndexServer struct {
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      SpatialIndex
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	grid        [256]map[uint32]data.Value
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		clientConns: map[string]*rpc.Conn{},
		grid:        [256]map[uint32]data.Value{},
	}

	for i := 0; i < 256; i++ {
		idx.grid[i] = make(map[uint32]data.Value)
	}

	for _, opt := range opts {
		opt(idx)
	}

	validate.Require(map[string]any{
		"ctx":  idx.ctx,
		"grid": idx.grid,
	})

	idx.serverSide, idx.clientSide = net.Pipe()
	idx.client = SpatialIndex_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		idx.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx
}

/*
Client returns a Cap'n Proto client connected to this SpatialIndexServer.
*/
func (idx *SpatialIndexServer) Client(clientID string) SpatialIndex {
	idx.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		idx.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (idx *SpatialIndexServer) Close() error {
	if idx.serverConn != nil {
		_ = idx.serverConn.Close()
		idx.serverConn = nil
	}

	for clientID, conn := range idx.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(idx.clientConns, clientID)
	}

	if idx.serverSide != nil {
		_ = idx.serverSide.Close()
		idx.serverSide = nil
	}
	if idx.clientSide != nil {
		_ = idx.clientSide.Close()
		idx.clientSide = nil
	}
	if idx.cancel != nil {
		idx.cancel()
	}

	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

func (idx *SpatialIndexServer) Write(
	ctx context.Context, call SpatialIndex_write,
) error {
	key := call.Args().Key()
	pos, symbol := morton.Unpack(key)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, exists := idx.grid[symbol][pos]; exists {
		return nil
	}

	idx.grid[symbol][pos] = data.Value{}

	return nil
}

func (idx *SpatialIndexServer) Lookup(
	ctx context.Context,
	call SpatialIndex_lookup,
) error {
	args := call.Args()

	keys := errnie.SafeMust(func() (capnp.UInt64List, error) {
		return args.Keys()
	})

	res := errnie.SafeMust(func() (SpatialIndex_lookup_Results, error) {
		return call.AllocResults()
	})
	
	out := errnie.SafeMust(func() (data.Value_List, error) {
		return res.NewValues(int32(keys.Len()))
	})

	for i := range keys.Len() {
		pos, symbol := morton.Unpack(keys.At(i))
		
		idx.mu.RLock()
		value, exists := idx.grid[symbol][pos]
		idx.mu.RUnlock()

		if exists {
			el := out.At(i)
			el.CopyFrom(value)
		}
	}

	return nil
}

/*
Entries returns all stored spatial entries.
*/
func (idx *SpatialIndexServer) Entries() []SpatialEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var entries []SpatialEntry
	for symbol, grid := range idx.grid {
		for pos, val := range grid {
			entries = append(entries, SpatialEntry{
				Key:   morton.Pack(pos, byte(symbol)),
				Value: val,
			})
		}
	}

	return entries
}

func WithContext(ctx context.Context) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.ctx = ctx
	}
}
