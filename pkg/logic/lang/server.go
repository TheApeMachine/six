package lang

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
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
		sink:        telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(server)
	}

	if server.ctx == nil || server.cancel == nil {
		server.ctx, server.cancel = context.WithCancel(context.Background())
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":  server.ctx,
			"sink": server.sink,
		})
	})

	if server.state.Failed() {
		return server
	}

	server.serverSide, server.clientSide = net.Pipe()
	server.client = Evaluator_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		server.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server
}

/*
Client returns a Cap'n Proto client connected to this ProgramServer.
*/
func (server *ProgramServer) Client(clientID string) Evaluator {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe.
*/
func (server *ProgramServer) Close() error {
	server.state.Reset()

	if server.serverConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverConn.Close()
		})

		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			errnie.GuardVoid(server.state, func() error {
				return conn.Close()
			})
		}

		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverSide.Close()
		})

		server.serverSide = nil
	}

	if server.clientSide != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.clientSide.Close()
		})

		server.clientSide = nil
	}

	if server.cancel != nil {
		server.cancel()
	}

	return server.state.Err()
}

/*
Write appends streamed native program Values into the server buffer.
*/
func (server *ProgramServer) Write(ctx context.Context, call Evaluator_write) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	seeds := errnie.Guard(server.state, func() ([]data.Value, error) {
		list, err := call.Args().Seed()
		if err != nil {
			return nil, err
		}

		return data.ValueListToSlice(list)
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	server.values = append(server.values, seeds...)

	return nil
}

/*
Done finalizes the current streamed program boundary.
*/
func (server *ProgramServer) Done(ctx context.Context, call Evaluator_done) error {
	server.state.Reset()

	_, err := call.AllocResults()
	if err != nil {
		return err
	}

	return nil
}

/*
Values returns a copy of the buffered native program values.
*/
func (server *ProgramServer) Values() []data.Value {
	server.mu.RLock()
	defer server.mu.RUnlock()

	values := make([]data.Value, len(server.values))

	for index, value := range server.values {
		copyValue := data.MustNewValue()
		copyValue.CopyFrom(value)
		values[index] = copyValue
	}

	return values
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
