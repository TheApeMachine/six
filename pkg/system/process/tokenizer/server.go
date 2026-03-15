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
	currentState  int
	calc          *numeric.Calculus
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
Generate implements Universal_Server. It tokenizes raw bytes into a
byte-addressable stream of chords. Every byte keeps its own Morton address,
while the Sequencer annotates boundary bytes with control-plane metadata.
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
tokenize runs the Sequencer over the byte stream while keeping storage byte-addressable.
Every emitted edge still names a concrete byte, but the position is now boundary-local:
the local depth resets whenever the Sequencer commits a cut. The chord itself remains
a transient observable carrying lexical seed + phase so prompts can still be injected
directly, while the spatial index strips that lexical seed before persistence.
*/
func (server *UniversalServer) tokenize(ctx context.Context, raw []byte) ([]TokenEdge, error) {
	_ = ctx

	sequitur := sequencer.NewSequitur()
	edges := make([]TokenEdge, 0, len(raw))
	state := numeric.Phase(1)
	localPos := uint32(0)

	for idx, currentByte := range raw {
		state = server.calc.Multiply(
			state,
			server.calc.Power(3, uint32(currentByte)),
		)

		isBoundary, _, _, emitMeta := sequitur.Analyze(uint32(idx), currentByte)

		value := data.NeutralValue()
		value.SetStatePhase(state)

		if idx+1 < len(raw) {
			value.SetLexicalTransition(raw[idx+1])
		}

		value.SetProgram(data.OpcodeNext, 1, 0, false)
		if isBoundary {
			value.SetProgram(data.OpcodeReset, 0, 1, false)
		}
		if idx == len(raw)-1 {
			value.SetProgram(data.OpcodeHalt, 0, value.Branches(), true)
		}

		chord := data.SeedObservable(currentByte, value)

		meta := data.MustNewChord()
		if isBoundary {
			meta.CopyFrom(emitMeta)
		}

		var right uint8
		if idx+1 < len(raw) {
			right = raw[idx+1]
		}

		edges = append(edges, TokenEdge{
			Left:     currentByte,
			Right:    right,
			Position: localPos,
			Chord:    chord,
			Meta:     meta,
		})

		if isBoundary {
			localPos = 0
		} else {
			localPos++
		}
	}

	if flushed, _, _, flushMeta := sequitur.Flush(); flushed && len(edges) > 0 {
		edges[len(edges)-1].Meta.CopyFrom(flushMeta)
		if edges[len(edges)-1].Chord.Branches() == 0 {
			edges[len(edges)-1].Chord.SetBranches(1)
		}
	}

	return edges, nil
}

/*
processChunk encodes a full chunk into a resonant program chord. It is kept for
benchmarks and offline callers that still want a span-level representation, but the
live tokenizer now emits byte-addressable edges so reconstruction remains exact.
*/
func (server *UniversalServer) processChunk(chunk []byte, metaChord data.Chord) (data.Chord, error) {
	if len(chunk) == 0 {
		return data.MustNewChord(), nil
	}

	out, err := data.BuildChord(chunk)
	if err != nil {
		return data.Chord{}, err
	}

	state := numeric.Phase(1)
	for _, currentByte := range chunk {
		state = server.calc.Multiply(
			state,
			server.calc.Power(3, uint32(currentByte)),
		)
	}

	out.SetStatePhase(state)
	out.SetAffine(1, 0)
	out.SetProgram(data.OpcodeJump, uint32(len(chunk)), 0, false)

	if metaChord.ActiveCount() > 0 {
		out.SetProgram(data.OpcodeBranch, uint32(len(chunk)), 1, false)
	}

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
