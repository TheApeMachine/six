package tokenizer

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
	"github.com/theapemachine/six/pkg/validate"
)

type TokenEdge struct {
	Left     uint8
	Right    uint8
	Position uint32
	Chord    data.Chord
	Meta     data.Chord
}

/*
UniversalServer implements the Universal_Server interface.
It tokenizes raw bytes into chords using Sequitur boundary analysis.
It has no knowledge of any other server — the Machine orchestrates
where the chords go after tokenization.
*/
type UniversalServer struct {
	ctx          context.Context
	cancel       context.CancelFunc
	serverSide   net.Conn
	clientSide   net.Conn
	client       Universal
	serverConn   *rpc.Conn
	clientConns  map[string]*rpc.Conn
	pool         *pool.Pool
	dataset      provider.Dataset
	corpusStrings []string
	stateMu      sync.Mutex
	currentState  int
	calc         *numeric.Calculus
}

type universalOpts func(*UniversalServer)

/*
NewUniversalServer instantiates a UniversalServer.
*/
func NewUniversalServer(opts ...universalOpts) *UniversalServer {
	server := &UniversalServer{
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
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
Generate implements Universal_Server. It tokenizes raw bytes into chords
and returns them. The Sequitur boundary detector segments the byte stream;
each segment becomes a chord encoding its GF(257) path state.
*/
func (server *UniversalServer) Generate(ctx context.Context, call Universal_generate) error {
	rawData, err := call.Args().Data()
	if err != nil {
		return err
	}

	edges, err := server.tokenize(ctx, rawData)
	if err != nil {
		return err
	}

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	edgeList, err := res.NewEdges(int32(len(edges)))
	if err != nil {
		return err
	}

	for i, edge := range edges {
		el := edgeList.At(i)
		el.SetLeft(edge.Left)
		el.SetRight(edge.Right)
		el.SetPosition(edge.Position)

		if err := el.SetChord(edge.Chord); err != nil {
			return err
		}
		if err := el.SetMeta(edge.Meta); err != nil {
			return err
		}
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
tokenize runs the Sequitur boundary analysis over raw bytes and returns
the resulting chord slice.
*/
func (server *UniversalServer) tokenize(ctx context.Context, raw []byte) ([]TokenEdge, error) {
	sequitur := sequencer.NewSequitur()

	var edges []TokenEdge
	var chunk []byte
	var pending []chan *pool.Result

	var pos uint32 = 0
	var left uint8 = 0

	flush := func(buf []byte, meta data.Chord, right uint8) {
		if len(buf) == 0 {
			return
		}

		chunkCopy := append([]byte(nil), buf...)
		chunkPos := pos
		chunkLeft := left

		ch := server.pool.Schedule("tokenizer_chunk", func(ctx context.Context) (any, error) {
			chord, err := server.processChunk(chunkCopy, meta)
			if err != nil {
				return nil, err
			}
			return TokenEdge{
				Left:     chunkLeft,
				Right:    right,
				Position: chunkPos,
				Chord:    chord,
				Meta:     meta,
			}, nil
		})

		pending = append(pending, ch)

		if len(buf) > 0 {
			pos += uint32(len(buf))
			left = buf[len(buf)-1]
		}
	}

	for i, b := range raw {
		chunk = append(chunk, b)
		isBoundary, emitK, _, emitMeta := sequitur.Analyze(uint32(i), b)

		if isBoundary {
			if emitK > len(chunk) {
				emitK = len(chunk)
			}

			var right uint8 = 0
			if emitK < len(chunk) {
				right = chunk[emitK]
			} else if i+1 < len(raw) {
				right = raw[i+1]
			}

			if emitK > 0 {
				flush(chunk[:emitK], emitMeta, right)
			}

			if emitK >= len(chunk) {
				chunk = chunk[:0]
			} else if emitK > 0 {
				copy(chunk, chunk[emitK:])
				chunk = chunk[:len(chunk)-emitK]
			}
		}
	}

	for {
		isBoundary, emitK, _, emitMeta := sequitur.Flush()
		if !isBoundary {
			break
		}

		if emitK > len(chunk) {
			emitK = len(chunk)
		}

		var right uint8 = 0
		if emitK < len(chunk) {
			right = chunk[emitK]
		}

		if emitK > 0 {
			flush(chunk[:emitK], emitMeta, right)
		}

		if emitK >= len(chunk) {
			chunk = chunk[:0]
		} else if emitK > 0 {
			copy(chunk, chunk[emitK:])
			chunk = chunk[:len(chunk)-emitK]
		}
	}

	if len(chunk) > 0 {
		flush(chunk, data.Chord{}, 0)
	}

	for _, ch := range pending {
		result := <-ch

		if result != nil && result.Error != nil {
			return nil, result.Error
		}

		if result != nil {
			if edge, ok := result.Value.(TokenEdge); ok {
				edges = append(edges, edge)
			}
		}
	}

	return edges, nil
}

/*
processChunk encodes a chunk's GF(257) path state into a chord.
*/
func (server *UniversalServer) processChunk(chunk []byte, metaChord data.Chord) (data.Chord, error) {
	if len(chunk) == 0 {
		return data.Chord{}, nil
	}

	server.stateMu.Lock()

	if server.currentState == 0 {
		server.currentState = 1
	}

	var out data.Chord

	for _, currentByte := range chunk {
		server.currentState = int(
			server.calc.Multiply(numeric.Phase(server.currentState),
				server.calc.Power(3, uint32(currentByte)),
			),
		)

		out = data.BaseChord(currentByte)
		out.Set(server.currentState)
	}

	server.stateMu.Unlock()

	return out, nil
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
