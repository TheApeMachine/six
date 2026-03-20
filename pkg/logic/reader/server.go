package reader

import (
	context "context"
	"net"
	"sync"

	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/validate"
)

type RegisterType uint

const (
	DATA RegisterType = iota
	CONTEXT
)

/*
ReaderServer reads prefetched values inside the graph substrate.
It keeps the address plane passive by operating only on materialized s,
evaluating phase checks, and optional bridge synthesis.
*/
type ReaderServer struct {
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	state       *errnie.State
	serverSide  net.Conn
	clientSide  net.Conn
	client      Head
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	registers   map[RegisterType][]primitive.Value
}

type readerOpts func(*ReaderServer)

/*
NewReaderServer instantiates a graph-local reader controller for prefetched
value s.
*/
func NewReaderServer(opts ...readerOpts) *ReaderServer {
	rdr := &ReaderServer{
		state: errnie.NewState("logic/reader/server"),
		registers: map[RegisterType][]primitive.Value{
			DATA:    []primitive.Value{},
			CONTEXT: []primitive.Value{},
		},
	}

	for _, opt := range opts {
		opt(rdr)
	}

	errnie.GuardVoid(rdr.state, func() error {
		return validate.Require(map[string]any{
			"ctx":       rdr.ctx,
			"registers": rdr.registers,
		})
	})

	return rdr
}

/*
Client returns a Cap'n Proto client connected to this GraphServer.
*/
func (rdr *ReaderServer) Client(clientID string) Reader {
	rdr.clientConns[clientID] = nil
	return Reader_ServerToClient(rdr)
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (rdr *ReaderServer) Close() error {
	rdr.state.Reset()
	return rdr.state.Err()
}

/*
Write data to the Reader, so we can start the recursive folding process, which uses
XOR and POPCNT to cancel out shared components and identify unique residues, building
up a labeled graph.
*/
func (rdr *ReaderServer) Write(
	ctx context.Context, call Reader_write,
) error {
	args := call.Args()

	values := errnie.Guard(rdr.state, func() (
		primitive.Value_List, error,
	) {
		return args.Values()
	})

	for idx := range values.Len() {
		value := values.At(idx)
		rdr.registers[DATA] = append(rdr.registers[DATA], value)
	}

	return nil
}

/*
Done implements Graph_Server. It is a no-op stub required by the RPC interface;
stream finalization is handled elsewhere (Machine orchestrates tokenizer and graph).
*/
func (rdr *ReaderServer) Done(ctx context.Context, call Reader_done) error {
	rdr.mu.Lock()
	defer rdr.mu.Unlock()

	return nil
}

func ReaderWithContext(ctx context.Context) readerOpts {
	return func(rdr *ReaderServer) {
		rdr.ctx, rdr.cancel = context.WithCancel(ctx)
	}
}
