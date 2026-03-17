package transport

import (
	"context"
	"net"

	"capnproto.org/go/capnp/v3/rpc"
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
	tcpConn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	rpcConn := rpc.NewConn(rpc.NewStreamTransport(tcpConn), nil)

	return rpcConn, nil
}
