package transport

import (
	"context"
	"net"

	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Dial connects to a remote Cap'n Proto RPC server at the given address
and returns the bootstrap client. The returned rpc.Conn must be closed
when the client is no longer needed.

Usage:

	client, conn, err := transport.Dial(ctx, "127.0.0.1:49201")
	defer conn.Close()
	typedClient := MyRPC(client)
*/
func Dial(ctx context.Context, addr string) (*rpc.Conn, error) {
	tcpConn := errnie.Guard(errnie.NewState("transport/dialer"), func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	})

	rpcConn := rpc.NewConn(rpc.NewStreamTransport(tcpConn), nil)

	return rpcConn, nil
}
