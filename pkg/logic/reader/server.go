package reader

import (
	"context"
	"fmt"
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
		state:       errnie.NewState("logic/reader/server"),
		clientConns: make(map[string]*rpc.Conn),
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
			"ctx": rdr.ctx,
		})
	})

	return rdr
}

/*
Client returns a Cap'n Proto client connected to this GraphServer.
*/
func (rdr *ReaderServer) Client(clientID string) Reader {
	if rdr.clientConns == nil {
		rdr.clientConns = make(map[string]*rpc.Conn)
	}

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
	_ = ctx

	args := call.Args()

	values := errnie.Guard(rdr.state, func() (
		primitive.Value_List, error,
	) {
		return args.Values()
	})

	if rdr.state.Failed() {
		return rdr.state.Err()
	}

	n := int(values.Len())

	for idx := 0; idx < n; idx++ {
		src := values.At(idx)

		if !src.IsValid() {
			return fmt.Errorf("reader: invalid value at index %d", idx)
		}

		slot, err := primitive.NewValue(src.Segment())
		if err != nil {
			return err
		}

		slot.CopyFrom(src)
		rdr.registers[DATA] = append(rdr.registers[DATA], slot)
	}

	return nil
}

/*
Done implements Reader.done and returns streaming completion metadata.
*/
func (rdr *ReaderServer) Done(ctx context.Context, call Reader_done) error {
	rdr.mu.Lock()
	defer rdr.mu.Unlock()

	_ = ctx

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	result, err := res.NewResult()
	if err != nil {
		return err
	}

	result.SetCount(uint64(len(rdr.registers[DATA])))
	_ = result.SetStatus("ok")

	return nil
}

func ReaderWithContext(ctx context.Context) readerOpts {
	return func(rdr *ReaderServer) {
		rdr.ctx, rdr.cancel = context.WithCancel(ctx)
	}
}
