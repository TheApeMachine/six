package transport

import (
	"context"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
Listener wraps a TCP listener that accepts Cap'n Proto RPC connections.
Each server embeds a *Listener instead of managing net.Pipe, serverSide,
clientSide, serverConn, and clientConns manually.

Usage:

	listener, err := transport.NewListener(ctx, ":0", MyRPC_ServerToClient(server))
	// listener.Addr() returns the bound address (e.g. "127.0.0.1:49201")

	// Connect locally or remotely:
	client, conn, err := transport.Dial[MyRPC](ctx, listener.Addr())
*/
type Listener struct {
	state    *errnie.State
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	addr     string
	client   capnp.Client
	conns    []*rpc.Conn
	pool     *pool.Pool
	mu       sync.Mutex
}

/*
listenerTask keeps the accept loop runnable by the pool.
*/
type listenerTask struct {
	listener *Listener
}

func (task *listenerTask) Read(p []byte) (n int, err error) {
	task.listener.accept()
	return 0, io.EOF
}

func (task *listenerTask) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (task *listenerTask) Close() error {
	return nil
}

/*
NewListener starts a TCP listener on the given address and begins
accepting Cap'n Proto RPC connections in the background. The bootstrap
client is provided to every incoming connection.

Use ":0" for an OS-assigned port.
*/
func NewListener(ctx context.Context, addr string, bootstrap capnp.Client) (*Listener, error) {
	tcpListener := errnie.Guard(errnie.NewState("transport/listener"), func() (net.Listener, error) {
		return net.Listen("tcp", addr)
	})

	ctx, cancel := context.WithCancel(ctx)

	listener := &Listener{
		state:    errnie.NewState("transport/listener"),
		ctx:      ctx,
		cancel:   cancel,
		listener: tcpListener,
		addr:     tcpListener.Addr().String(),
		client:   bootstrap,
		pool: pool.New(
			ctx,
			1,
			runtime.NumCPU(),
			&pool.Config{},
		),
	}

	_ = listener.pool.Schedule(
		"transport/listener/accept",
		pool.COMPUTE,
		&listenerTask{listener: listener},
		pool.WithContext(listener.ctx),
		pool.WithTTL(time.Second),
	)

	return listener, nil
}

/*
Addr returns the bound TCP address (e.g. "127.0.0.1:49201").
*/
func (listener *Listener) Addr() string {
	return listener.addr
}

/*
accept runs the connection accept loop. Each accepted connection gets
its own rpc.Conn with the bootstrap client.
*/
func (listener *Listener) accept() {
	for {
		conn := errnie.Guard(listener.state, func() (net.Conn, error) {
			return listener.listener.Accept()
		})

		if listener.state.Failed() {
			return
		}

		rpcConn := rpc.NewConn(rpc.NewStreamTransport(conn), &rpc.Options{
			BootstrapClient: listener.client,
		})

		listener.mu.Lock()
		listener.conns = append(listener.conns, rpcConn)
		listener.mu.Unlock()
	}
}

/*
Close shuts down the listener and all active RPC connections.
*/
func (listener *Listener) Close() error {
	listener.cancel()
	_ = listener.listener.Close()

	listener.mu.Lock()
	defer listener.mu.Unlock()

	for _, conn := range listener.conns {
		_ = conn.Close()
	}

	listener.conns = nil
	if listener.pool != nil {
		_ = listener.pool.Close()
	}

	return nil
}
