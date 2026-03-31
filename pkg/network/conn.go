package network

import (
	"context"
	"io"
	"sync"
)

/*
UniConnType selects the transport layer backing a UniConn.
*/
type UniConnType uint

const (
	IPCType UniConnType = iota
	UDPType
	QUICType
)

/*
UniConn is a unified network connection that delegates to a concrete
transport (IPC shared memory, UDP multicast, or QUIC) selected at
construction time. It implements io.ReadWriteCloser so that Value
frames flow through the same interface regardless of the underlying wire.
*/
type UniConn struct {
	err          error
	ctx          context.Context
	cancel       context.CancelFunc
	transports   map[UniConnType]io.ReadWriteCloser
	sources      io.Writer
	destinations io.Reader
	readyErr     error
	ready        sync.Once
}

/*
uniConnOption configures a UniConn at construction time.
*/
type uniConnOption func(*UniConn)

/*
NewUniConn constructs a UniConn. Without options it has no transport;
pass UniConnWithIPC, UniConnWithUDP, or UniConnWithQUIC to wire one up.
*/
func NewUniConn(ctx context.Context, opts ...uniConnOption) *UniConn {
	ctx, cancel := context.WithCancel(ctx)

	conn := &UniConn{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(conn)
	}

	return conn
}

/*
Read delegates to the underlying transport.
*/
func (conn *UniConn) Read(p []byte) (int, error) {
	if err := conn.ensureReady(); err != nil {
		return 0, err
	}

	return conn.destinations.Read(p)
}

/*
Write delegates to the underlying transport.
*/
func (conn *UniConn) Write(p []byte) (int, error) {
	if err := conn.ensureReady(); err != nil {
		return 0, err
	}

	return conn.sources.Write(p)
}

// Ready blocks until the configured transport reaches a protocol-specific
// ready state. For IPC/UDP this is immediate; QUIC listener mode accepts and
// primes the first stream under the hood.
func (conn *UniConn) Ready() error {
	return conn.ensureReady()
}

/*
Close tears down the connection context and the underlying transport.
*/
func (conn *UniConn) Close() error {
	conn.cancel()

	if conn.sources == nil && conn.destinations == nil {
		return nil
	}

	return nil
}

func (conn *UniConn) ensureReady() error {
	if conn.sources == nil && conn.destinations == nil {
		return NewNetworkError(ErrTransportFailure)
	}

	conn.ready.Do(func() {
		if ready, ok := conn.sources.(ReadyTransport); ok {
			conn.readyErr = ready.Ready(conn.ctx)
		}
	})

	return conn.readyErr
}

/*
UniConnWithContext replaces the default background context.
*/
func UniConnWithContext(ctx context.Context) uniConnOption {
	return func(conn *UniConn) {
		conn.cancel()
		conn.ctx, conn.cancel = context.WithCancel(ctx)
	}
}

/*
UniConnWithTransport wires a new transport.
*/
func UniConnWithTransport(
	transportType *IPC, transport io.ReadWriteCloser,
) uniConnOption {
	return func(conn *UniConn) {
		conn.transports[IPCType] = transport
	}
}

/*
UniConnError is a typed error for UniConn failures.
*/
type UniConnError string

const (
	ErrNoTransport UniConnError = "uniconn: no transport configured"
)

/*
Error implements the error interface for UniConnError.
*/
func (connErr UniConnError) Error() string {
	return string(connErr)
}
