package tokenizer

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
	"github.com/theapemachine/six/pkg/validate"
)

/*
UniversalServer implements the Universal_Server interface.
It tokenizes raw bytes into Morton keys using Sequitur boundary analysis.
The key IS the observable: (byte_value << 32 | localDepth).
No values are produced here — downstream stages compile the key stream into
native value-plane operators before insertion into the spatial index.
*/
type UniversalServer struct {
	ctx           context.Context
	cancel        context.CancelFunc
	serverSide    net.Conn
	clientSide    net.Conn
	client        Universal
	serverConn    *rpc.Conn
	clientConns   map[string]*rpc.Conn
	pool          *pool.Pool
	dataset       provider.Dataset
	corpusStrings []string
	stateMu       sync.Mutex
	morton        *data.MortonCoder
}

type universalOpts func(*UniversalServer)

/*
NewUniversalServer instantiates a UniversalServer.
*/
func NewUniversalServer(opts ...universalOpts) *UniversalServer {
	server := &UniversalServer{
		clientConns: map[string]*rpc.Conn{},
		morton:      data.NewMortonCoder(),
	}

	for _, opt := range opts {
		opt(server)
	}

	validate.Require(map[string]any{
		"ctx":  server.ctx,
		"pool": server.pool,
	})

	server.serverSide, server.clientSide = net.Pipe()
	server.client = Universal_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		server.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server
}

/*
Client returns a Cap'n Proto client connected to this UniversalServer.
*/
func (server *UniversalServer) Client(clientID string) Universal {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *UniversalServer) Close() error {
	if server.serverConn != nil {
		_ = server.serverConn.Close()
		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		_ = server.serverSide.Close()
		server.serverSide = nil
	}
	if server.clientSide != nil {
		_ = server.clientSide.Close()
		server.clientSide = nil
	}
	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Generate implements Universal_Server. It tokenizes raw bytes into Morton keys.
Each byte becomes a key: (byte_value << 32 | localDepth). The Sequencer
determines where localDepth resets to 0 (boundary detection). Native values are
compiled later so the tokenizer remains purely address-plane logic.
*/
func (server *UniversalServer) Generate(ctx context.Context, call Universal_generate) error {
	rawData, err := call.Args().Data()
	if err != nil {
		return err
	}

	keys := server.tokenize(rawData)

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	keyList, err := res.NewKeys(int32(len(keys)))
	if err != nil {
		return err
	}

	for i, key := range keys {
		keyList.Set(i, key)
	}

	return nil
}

/*
Done implements Universal_Server.
*/
func (server *UniversalServer) Done(ctx context.Context, call Universal_done) error {
	return nil
}

/*
SetDataset implements Universal_Server. Stores corpus strings so the Machine
can drive ingest over them via subsequent Generate calls.
*/
func (server *UniversalServer) SetDataset(ctx context.Context, call Universal_setDataset) error {
	corpus, err := call.Args().Corpus()
	if err != nil {
		return err
	}

	strings := make([]string, corpus.Len())

	for i := 0; i < corpus.Len(); i++ {
		str, err := corpus.At(i)
		if err != nil {
			return err
		}

		strings[i] = str
	}

	server.stateMu.Lock()
	server.corpusStrings = strings
	server.stateMu.Unlock()

	return nil
}

/*
tokenize runs the Sequencer over the byte stream and emits Morton keys.
Each byte produces one key: Pack(localDepth, byte). The Sequencer
resets localDepth to 0 at each structural boundary.
*/
func (server *UniversalServer) tokenize(raw []byte) []uint64 {
	seq := sequencer.NewSequitur()
	keys := make([]uint64, 0, len(raw))
	localPos := uint32(0)

	for idx, currentByte := range raw {
		isBoundary, _, _, _ := seq.Analyze(uint32(idx), currentByte)

		keys = append(keys, server.morton.Pack(localPos, currentByte))

		if isBoundary {
			localPos = 0
		} else {
			localPos++
		}
	}

	return keys
}

/*
Tokenize is the direct non-RPC entry point for tokenization.
Returns Morton keys for the given raw byte stream.
*/
func (server *UniversalServer) Tokenize(raw []byte) []uint64 {
	return server.tokenize(raw)
}

/*
UniversalWithContext sets a cancellable context on the server.
*/
func UniversalWithContext(ctx context.Context) universalOpts {
	return func(server *UniversalServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
UniversalWithPool injects the shared worker pool.
*/
func UniversalWithPool(workerPool *pool.Pool) universalOpts {
	return func(server *UniversalServer) {
		server.pool = workerPool
	}
}

/*
UniversalWithDataset injects a dataset for training ingestion.
*/
func UniversalWithDataset(dataset provider.Dataset) universalOpts {
	return func(server *UniversalServer) {
		server.dataset = dataset
	}
}

/*
UniversalError is a typed error for UniversalServer failures.
*/
type UniversalError string

const (
	ErrNoIndex         UniversalError = "spatial index capability not yet received"
	ErrCollectorSample UniversalError = "collector sample index out of range"
)

/*
Error implements the error interface for UniversalError.
*/
func (err UniversalError) Error() string {
	return string(err)
}
