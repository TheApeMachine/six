package process

import (
	"context"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/provider"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
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
	sink          *telemetry.Sink
	dataset       provider.Dataset
}

type tokenizerOpts func(*TokenizerServer)

/*
NewTokenizerServer instantiates a TokenizerServer.
*/
func NewTokenizerServer(opts ...tokenizerOpts) *TokenizerServer {
	server := &TokenizerServer{
		sink: telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(server)
	}

	validate.Require(map[string]any{
		"pool":      server.pool,
		"broadcast": server.broadcast,
		"dataset":   server.dataset,
	})

	return server
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect.
*/
func (server *TokenizerServer) Announce() {
	console.Info("Announcing Tokenizer")

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
			console.Info("tokenizer picked up spatial index")
			server.spatialInsert = pv.Value
		}
	}
}

/*
Generate implements the Tokenizer_Server.Generate RPC method.
The dataset is injected at construction time; the RPC call is the trigger.
*/
func (server *TokenizerServer) Generate(ctx context.Context, call Tokenizer_generate) error {
	return server.generate(ctx)
}

/*
Done implements the Tokenizer_Server.Done RPC method.
*/
func (server *TokenizerServer) Done(ctx context.Context, call Tokenizer_done) error {
	return nil
}

func (server *TokenizerServer) generate(ctx context.Context) error {
	seq := NewSequencer(NewCalibrator())

	if server.spatialInsert == nil {
		return console.Error(ErrNoIndex)
	}

	var chunk []byte

	console.Info("Tokenizer generating dataset")

	for token := range server.dataset.Generate() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chunk = append(chunk, token.Symbol)
		isBoundary, _ := seq.Analyze(token.Pos, token.Symbol)

		if isBoundary {
			server.sink.Emit(telemetry.Event{
				Component: "Sequencer",
				Action:    "Boundary",
			})

			c := append([]byte(nil), chunk...)
			server.pool.Schedule("tokenizer_process_chunk", func() (any, error) {
				server.processChunk(ctx, c)
				return nil, nil
			})
			chunk = chunk[:0]
		}
	}

	if len(chunk) > 0 {
		c := append([]byte(nil), chunk...)
		server.pool.Schedule("tokenizer_process_chunk", func() (any, error) {
			server.processChunk(ctx, c)
			return nil, nil
		})
	}

	return nil
}

func (server *TokenizerServer) handleChunk(ctx context.Context, chunk []byte) {
	c := append([]byte(nil), chunk...)
	server.pool.Schedule("tokenizer_process_chunk", func() (any, error) {
		server.processChunk(ctx, c)
		return nil, nil
	})
	chunk = chunk[:0]
}

func (server *TokenizerServer) processChunk(ctx context.Context, chunk []byte) {
	if len(chunk) < 2 || server.spatialInsert == nil {
		return
	}

	for i, currentByte := range chunk {
		cumulativeChord, _ := data.BuildChord(chunk[:i+1])
		_ = server.spatialInsert(ctx, currentByte, uint32(i), cumulativeChord)

		if i < len(chunk)-1 {
			server.sink.Emit(telemetry.Event{
				Component: "LSM",
				Action:    "Insert",
				Data: telemetry.EventData{
					Left:  int(chunk[i]),
					Right: int(chunk[i+1]),
					Pos:   i,
				},
			})
		}
	}

	if server.broadcast == nil {
		return
	}

	sequenceChord, _ := data.BuildChord(chunk)

	server.sink.Emit(telemetry.Event{
		Component: "Tokenizer",
		Action:    "Chord",
		Data: telemetry.EventData{
			Bin:        data.ChordBin(&sequenceChord),
			State:      "stored",
			ActiveBits: data.ChordPrimeIndices(&sequenceChord),
			Density:    sequenceChord.ShannonDensity(),
			ChunkText:  string(chunk),
		},
	})

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
TokenizerWithDataset injects a dataset.
*/
func TokenizerWithDataset(dataset provider.Dataset) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.dataset = dataset
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
