package tokenizer

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
	"github.com/theapemachine/six/pkg/validate"
)

/*
UniversalServer tokenizes raw bytes into Morton keys.
Bytes in → keys out via Done.
*/
type UniversalServer struct {
	state       *errnie.State
	ctx         context.Context
	cancel      context.CancelFunc
	serverConn  *rpc.Conn
	clientConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	pool        *pool.Pool
	seq         *sequencer.Sequitur
	pos         uint32
	stateMu     sync.Mutex
	connMu      sync.Mutex
	morton      *data.MortonCoder
	healer      *sequencer.BitwiseHealer
	sequences   [][]byte
}

type universalOpts func(*UniversalServer)

/*
NewUniversalServer instantiates a UniversalServer.
*/
func NewUniversalServer(opts ...universalOpts) *UniversalServer {
	server := &UniversalServer{
		state:       errnie.NewState("tokenizer/server"),
		clientConns: map[string]*rpc.Conn{},
		morton:      data.NewMortonCoder(),
		seq:         sequencer.NewSequitur(),
		healer:      sequencer.NewBitwiseHealer(),
	}

	for _, opt := range opts {
		opt(server)
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx": server.ctx,
			"seq": server.seq,
		})
	})

	serverSide, clientSide := net.Pipe()
	capability := Universal_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(capability),
	})

	server.clientConn = rpc.NewConn(rpc.NewStreamTransport(clientSide), nil)

	return server
}

/*
Client returns a Cap'n Proto client connected to this UniversalServer.
Returns the bootstrap capability from the pre-created client connection.
This connection is shared by all clients for proper RPC multiplexing.
*/
func (server *UniversalServer) Client(clientID string) capnp.Client {
	server.connMu.Lock()
	server.clientConns[clientID] = server.clientConn
	server.connMu.Unlock()

	return server.clientConn.Bootstrap(server.ctx)
}

/*
Load approximates concurrent RPC pressure via active client registrations.
*/
func (server *UniversalServer) Load() int64 {
	return int64(len(server.clientConns))
}

/*
Close shuts down the RPC connections and underlying net.Pipe.
*/
func (server *UniversalServer) Close() error {
	server.state.Reset()

	if server.clientConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.clientConn.Close()
		})

		server.clientConn = nil
	}

	if server.serverConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverConn.Close()
		})

		server.serverConn = nil
	}

	server.connMu.Lock()
	for clientID := range server.clientConns {
		delete(server.clientConns, clientID)
	}
	server.connMu.Unlock()

	if server.cancel != nil {
		server.cancel()
	}

	return server.state.Err()
}

/*
Write implements Universal_Server. Bytes are buffered into sequencer fragments;
healed Morton keys are emitted only when the surrounding sequence is finalized.
*/
func (server *UniversalServer) Write(ctx context.Context, call Universal_write) error {
	server.stateMu.Lock()
	defer server.stateMu.Unlock()

	server.tokenize(call.Args().Data())
	return server.state.Err()
}

/*
Feedback receives an error correction signal from the GraphServer.
This is reserved for a future implementation.
*/
func (server *UniversalServer) Feedback(
	ctx context.Context, call Universal_feedback,
) error {
	return nil
}

/*
Done implements Universal_Server. It flushes any residual bytes from the
healer, encodes every healed sequence as a Morton key stream, and packs
them into the response for the machine to consume.
*/
func (server *UniversalServer) Done(
	ctx context.Context, call Universal_done,
) error {
	server.stateMu.Lock()
	defer server.stateMu.Unlock()

	server.state.Reset()

	flushed, flushErr := server.healer.Flush()
	if flushErr != nil {
		server.state.Handle(flushErr)

		return server.state.Err()
	}

	for _, seq := range flushed {
		server.sequences = append(server.sequences, seq)
	}

	var keys []uint64

	for _, seq := range server.sequences {
		for pos, symbol := range seq {
			keys = append(keys, server.morton.Pack(uint32(pos), symbol))
		}
	}

	server.sequences = server.sequences[:0]
	server.seq = sequencer.NewSequitur()
	server.healer = sequencer.NewBitwiseHealer()
	server.pos = 0

	res := errnie.Guard(server.state, func() (Universal_done_Results, error) {
		return call.AllocResults()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	keyList := errnie.Guard(server.state, func() (capnp.UInt64List, error) {
		return res.NewKeys(int32(len(keys)))
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	for index, key := range keys {
		keyList.Set(index, key)
	}

	return server.state.Err()
}

/*
SetDataset implements Universal_Server.
*/
func (server *UniversalServer) SetDataset(
	ctx context.Context, call Universal_setDataset,
) error {
	server.state.Reset()

	corpus := errnie.Guard(server.state, func() (capnp.TextList, error) {
		return call.Args().Corpus()
	})

	strings := make([]string, corpus.Len())

	for i := 0; i < corpus.Len(); i++ {
		strings[i] = errnie.Guard(server.state, func() (string, error) {
			return corpus.At(i)
		})
	}

	return server.state.Err()
}

/*
tokenize runs the Sequencer over one byte and returns a Morton key.
*/
func (server *UniversalServer) tokenize(raw byte) {
	server.healer.Write(server.seq.Analyze(server.pos, raw))
	server.pos++

	buf, healErr := server.healer.Heal()
	if healErr != nil {
		server.state.Handle(healErr)

		return
	}

	if buf != nil {
		for _, seq := range buf {
			server.sequences = append(server.sequences, seq)
		}
	}
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
UniversalError is a typed error for UniversalServer failures.
*/
type UniversalError string

const (
	ErrFragmentDrift UniversalError = "sequencer fragment length drift"
	ErrHealerDrift   UniversalError = "bitwise healer buffer drift"
	ErrHealedLength  UniversalError = "bitwise healer changed sequence length"
)

/*
Error implements the error interface for UniversalError.
*/
func (err UniversalError) Error() string {
	return string(err)
}
