package process

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/provider"
	"github.com/theapemachine/six/pkg/provider/local"
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
	useSampleID   bool
	currentSample uint32
	collector     [][]data.Chord
	collectorMu   sync.Mutex
	ready         atomic.Bool
	clientConn    net.Conn
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
		"dataset": server.dataset,
	})

	return server
}

/*
Start implements the vm.System interface.
*/
func (server *TokenizerServer) Start(workerPool *pool.Pool, broadcast *pool.BroadcastGroup) {
	server.pool = workerPool
	server.broadcast = broadcast
}

func (server *TokenizerServer) Ready() bool {
	return server.ready.Load()
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

	server.clientConn = clientSide

	server.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[net.Conn]{
			Key:   "tokenizer",
			Value: clientSide,
		},
	})
}

/*
Close cleans up resources like RPC and cancel functions.
*/
func (server *TokenizerServer) Close() error {
	if server.cancel != nil {
		server.cancel()
	}
	if server.rpcConn != nil {
		server.rpcConn.Close()
		server.rpcConn = nil
	}
	if server.clientConn != nil {
		server.clientConn.Close()
		server.clientConn = nil
	}
	return nil
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
	calibrator := NewCalibrator()
	seq := NewSequencer(calibrator)
	activeCtx := ctx
	server.ready.Store(false)

	if server.ctx != nil {
		activeCtx = server.ctx
	}

	if server.spatialInsert == nil && server.collector == nil {
		return console.Error(ErrNoIndex)
	}

	var chunk []byte
	var pending []chan *pool.Result

	console.Info("Tokenizer generating dataset")

	flush := func(sampleID, startPos uint32, buf []byte) {
		if len(buf) == 0 {
			return
		}

		chunkCopy := append([]byte(nil), buf...)
		ch := server.pool.Schedule("tokenizer_process_chunk", func() (any, error) {
			return nil, server.processChunk(activeCtx, sampleID, startPos, chunkCopy)
		})
		pending = append(pending, ch)
	}

	flushSync := func(sampleID, startPos uint32, buf []byte) error {
		if len(buf) == 0 {
			return nil
		}

		if server.pool == nil {
			return server.processChunk(activeCtx, sampleID, startPos, append([]byte(nil), buf...))
		}

		flush(sampleID, startPos, buf)

		return nil
	}

	waitPending := func() error {
		for _, ch := range pending {
			result := <-ch

			if result != nil && result.Error != nil {
				return result.Error
			}
		}

		return nil
	}

	var chunkStart uint32
	var sampleBase uint32
	var sampleSpan uint32

	// flushSample drains any pending sequencer boundaries for the current sample
	// and emits the remaining buffered data as the terminal chunk.
	flushSample := func() error {
		for {
			isBoundary, emitK, _ := seq.Flush()
			if !isBoundary {
				break
			}

			server.sink.Emit(telemetry.Event{Component: "Sequencer", Action: "Boundary"})

			if err := flushSync(server.currentSample, chunkStart, chunk[:emitK]); err != nil {
				return err
			}

			copy(chunk, chunk[emitK:])
			chunk = chunk[:len(chunk)-emitK]
			chunkStart += uint32(emitK)
		}

		if err := flushSync(server.currentSample, chunkStart, chunk); err != nil {
			return err
		}

		chunk = chunk[:0]

		return nil
	}

	for token := range server.dataset.Generate() {
		select {
		case <-activeCtx.Done():
			if err := waitPending(); err != nil {
				return err
			}

			return activeCtx.Err()
		default:
		}

		if server.currentSample != token.SampleID {
			server.sink.Emit(telemetry.Event{Component: "Sequencer", Action: "Boundary"})

			if err := flushSample(); err != nil {
				return err
			}

			if !server.useSampleID {
				sampleBase += sampleSpan
			}

			server.currentSample = token.SampleID
			seq = seq.CloneEmpty()
			sampleSpan = 0
		}

		if len(chunk) == 0 {
			chunkStart = token.Pos

			if !server.useSampleID {
				chunkStart += sampleBase
			}
		}

		// token.Pos is zero-based, so the sample span is the furthest position plus one byte.
		sampleSpan = max(sampleSpan, token.Pos+1)
		chunk = append(chunk, token.Symbol)
		isBoundary, emitK, _ := seq.Analyze(token.Pos, token.Symbol)

		if isBoundary {
			server.sink.Emit(telemetry.Event{Component: "Sequencer", Action: "Boundary"})

			if err := flushSync(server.currentSample, chunkStart, chunk[:emitK]); err != nil {
				return err
			}

			copy(chunk, chunk[emitK:])
			chunk = chunk[:len(chunk)-emitK]
			chunkStart += uint32(emitK)
		}
	}

	if err := flushSample(); err != nil {
		return err
	}

	if err := waitPending(); err != nil {
		return err
	}

	server.ready.Store(true)

	return nil
}

func (server *TokenizerServer) processChunk(ctx context.Context, sampleID, startPos uint32, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}

	chunkChord, err := data.BuildChord(chunk)

	if err != nil {
		return console.Error(err)
	}

	if server.collector != nil {
		server.collectorMu.Lock()
		if int(sampleID) >= len(server.collector) {
			server.collectorMu.Unlock()
			return console.Error(ErrCollectorSample)
		}

		server.collector[sampleID] = append(server.collector[sampleID], chunkChord)
		server.collectorMu.Unlock()
	}

	for i, currentByte := range chunk {
		if !server.useSampleID && server.spatialInsert != nil {
			if err := server.spatialInsert(ctx, currentByte, startPos+uint32(i), chunkChord); err != nil {
				return console.Error(err)
			}
		}

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

	return nil
}

/*
TokenizeSingleSample is an API to tokenize a standalone sample, returning an error.
*/
func (server *TokenizerServer) TokenizeSingleSample(ctx context.Context, sample string) error {
	server.collector = [][]data.Chord{{}}
	server.currentSample = 0

	ds := local.New(local.WithStrings([]string{sample}))
	server.dataset = ds
	server.useSampleID = false

	return server.generate(ctx)
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
TokenizerWithDataset injects a dataset.
*/
func TokenizerWithDataset(dataset provider.Dataset, useSampleID bool) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.dataset = dataset
		server.useSampleID = useSampleID
	}
}

/*
TokenizerWithCollector injects a collector, used to collect prompts.
*/
func TokenizerWithCollector(collector [][]data.Chord) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.collector = collector
	}
}

/*
TokenizerError is a typed error for TokenizerServer failures.
*/
type TokenizerError string

const (
	ErrNoIndex         TokenizerError = "spatial index capability not yet received"
	ErrCollectorSample TokenizerError = "collector sample index out of range"
)

/*
Error implements the error interface for TokenizerError.
*/
func (tokErr TokenizerError) Error() string {
	return string(tokErr)
}
