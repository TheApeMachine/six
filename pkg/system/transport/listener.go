package transport

import (
	"context"
	"net"
	"sync"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
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
	mu       sync.Mutex
}

/*
NewListener starts a TCP listener on the given address and begins
accepting Cap'n Proto RPC connections in the background. The bootstrap
client is provided to every incoming connection.

Use ":0" for an OS-assigned port.
*/
func NewListener(ctx context.Context, addr string, bootstrap capnp.Client) (*Listener, error) {
	tcpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	listener := &Listener{
		state:    errnie.NewState("transport/listener"),
		ctx:      ctx,
		cancel:   cancel,
		listener: tcpListener,
		addr:     tcpListener.Addr().String(),
		client:   bootstrap,
	}

	go listener.accept()

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
		conn, err := listener.listener.Accept()
		if err != nil {
			select {
			case <-listener.ctx.Done():
				return
			default:
				listener.state.Handle(err)
				continue
			}
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

	return nil
}
