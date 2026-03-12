package process

import (
	"context"
	"errors"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/store/lsm"
)

/*
TokenizerServer implements the Tokenizer_Server interface.
The SpatialIndex insert capability is received as a typed function via the
broadcast bus — no raw capnp.Client, no method descriptors, no lsm internals.
*/
type TokenizerServer struct {
	ctx           context.Context
	cancel        context.CancelFunc
	pool          *pool.Pool
	broadcast     *pool.BroadcastGroup
	rpcConn       *rpc.Conn
	spatialInsert lsm.SpatialInsertFunc
}

type tokenizerOpts func(*TokenizerServer)

/*
NewTokenizerServer instantiates a TokenizerServer.
*/
func NewTokenizerServer(opts ...tokenizerOpts) *TokenizerServer {
	server := &TokenizerServer{}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect.
*/
func (server *TokenizerServer) Announce() {
	if server.broadcast == nil {
		return
	}

	serverSide, clientSide := net.Pipe()
	client := Tokenizer_ServerToClient(server)

	server.rpcConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	server.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[net.Conn]{
			Key:   "tokenizer",
			Value: clientSide,
		},
	})
}

/*
Receive implements the vm.System interface.
Picks up the SpatialInsertFunc from the broadcast bus once the spatial index
has announced itself.
*/
func (server *TokenizerServer) Receive(result *pool.Result) {
	if result == nil || result.Value == nil {
		return
	}

	if pv, ok := result.Value.(pool.PoolValue[lsm.SpatialInsertFunc]); ok {
		if pv.Key == lsm.SpatialInsertKey {
			server.spatialInsert = pv.Value
		}
	}
}

/*
Generate implements the Tokenizer_Server.Generate RPC method.
*/
func (server *TokenizerServer) Generate(ctx context.Context, call Tokenizer_generate) error {
	return errnie.Then(
		errnie.Try(call.Args().Raw()),
		func(raw []byte) ([]byte, error) {
			return raw, server.generate(ctx, raw)
		},
	).Err()
}

/*
Done implements the Tokenizer_Server.Done RPC method.
*/
func (server *TokenizerServer) Done(ctx context.Context, call Tokenizer_done) error {
	return nil
}

func (server *TokenizerServer) generate(ctx context.Context, raw []byte) error {
	if server.pool == nil {
		return errors.New("tokenizer pool is not configured")
	}

	server.pool.Schedule("tokenizer_generate", func() (any, error) {
		seq := NewSequencer(nil)
		var chunk []byte

		for pos, byteVal := range raw {
			chunk = append(chunk, byteVal)
			isBoundary, _ := seq.Analyze(pos, byteVal)

			if isBoundary {
				server.processChunk(ctx, chunk)
				chunk = nil
			}
		}

		if len(chunk) > 0 {
			server.processChunk(ctx, chunk)
		}

		return nil, nil
	})

	return nil
}

func (server *TokenizerServer) processChunk(ctx context.Context, chunk []byte) {
	if len(chunk) < 2 || server.spatialInsert == nil {
		return
	}

	for i, currentByte := range chunk {
		cumulativeChord, _ := data.BuildChord(chunk[:i+1])
		_ = server.spatialInsert(ctx, currentByte, uint32(i), cumulativeChord)
	}

	if server.broadcast == nil {
		return
	}

	sequenceChord, _ := data.BuildChord(chunk)

	server.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[[]data.Chord]{
			Key:   "prompt",
			Value: []data.Chord{sequenceChord},
		},
	})
}

/*
TokenizerWithContext sets a cancellable context on the server.
*/
func TokenizerWithContext(ctx context.Context) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
TokenizerWithPool injects the shared worker pool.
*/
func TokenizerWithPool(p *pool.Pool) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.pool = p
	}
}

/*
TokenizerWithBroadcast injects the broadcast group.
*/
func TokenizerWithBroadcast(broadcast *pool.BroadcastGroup) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.broadcast = broadcast
	}
}

/*
TokenizerError is a typed error for TokenizerServer failures.
*/
type TokenizerError string

const (
	ErrNoIndex TokenizerError = "spatial index capability not yet received"
)

/*
Error implements the error interface for TokenizerError.
*/
func (tokErr TokenizerError) Error() string {
	return string(tokErr)
}
