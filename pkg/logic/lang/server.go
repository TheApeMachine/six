package lang

import (
	"context"
	"net"
	"sync"

	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
ProgramServer is a composition of one or more programmable Value types.
In this architecture, the system programs itself via its native
language of Value types. This is the only way it is supposed to
solve problems, we do not hardcode the logic.
*/
type ProgramServer struct {
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	state       *errnie.State
	serverSide  net.Conn
	clientSide  net.Conn
	client      Evaluator
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	workerPool  *pool.Pool
	sink        *telemetry.Sink
	values      []data.Value
}

type programServerOpts func(*ProgramServer)

/*
NewProgramServer creates a new ProgramServer.
*/
func NewProgramServer(opts ...programServerOpts) *ProgramServer {
	server := &ProgramServer{
		state:       errnie.NewState("logic/lang/programServer"),
		clientConns: map[string]*rpc.Conn{},
	}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

/*
ProgramServerWithValues adds one or more Value types to the Program.
*/
func ProgramServerWithValues(values ...data.Value) programServerOpts {
	return func(server *ProgramServer) {
		server.values = values
	}
}

/*
func ProgramServerWithContext sets a cancellable context.
*/
func ProgramServerWithContext(ctx context.Context) programServerOpts {
	return func(server *ProgramServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
ProgramServerWithWorkerPool sets the shared worker pool.
*/
func ProgramServerWithWorkerPool(pool *pool.Pool) programServerOpts {
	return func(server *ProgramServer) {
		server.workerPool = pool
	}
}

/*
ProgramServerWithSink sets the telemetry sink.
*/
func ProgramServerWithSink(sink *telemetry.Sink) programServerOpts {
	return func(server *ProgramServer) {
		server.sink = sink
	}
}
