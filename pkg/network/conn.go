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
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	connType UniConnType
	rwc      io.ReadWriteCloser
	readyErr error
	ready    sync.Once
}

/*
uniConnOption configures a UniConn at construction time.
*/
type uniConnOption func(*UniConn)

/*
NewUniConn constructs a UniConn. Without options it has no transport;
pass UniConnWithIPC, UniConnWithUDP, or UniConnWithQUIC to wire one up.
*/
func NewUniConn(opts ...uniConnOption) *UniConn {
	ctx, cancel := context.WithCancel(context.Background())

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

	return conn.rwc.Read(p)
}

/*
Write delegates to the underlying transport.
*/
func (conn *UniConn) Write(p []byte) (int, error) {
	if err := conn.ensureReady(); err != nil {
		return 0, err
	}

	return conn.rwc.Write(p)
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

	if conn.rwc == nil {
		return nil
	}

	return conn.rwc.Close()
}

func (conn *UniConn) ensureReady() error {
	if conn.rwc == nil {
		return &TransportError{Layer: "uniconn", Op: "ready", Err: ErrNoTransport}
	}

	conn.ready.Do(func() {
		if ready, ok := conn.rwc.(ReadyTransport); ok {
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
UniConnWithIPC wires a shared-memory IPC transport.
*/
func UniConnWithIPC(transport *IPC) uniConnOption {
	return func(conn *UniConn) {
		conn.connType = IPCType
		conn.rwc = transport
	}
}

/*
UniConnWithUDP wires a UDP multicast transport.
*/
func UniConnWithUDP(transport *UDPMulticast) uniConnOption {
	return func(conn *UniConn) {
		conn.connType = UDPType
		conn.rwc = transport
	}
}

/*
UniConnWithQUIC wires a QUIC transport for WAN communication.
*/
func UniConnWithQUIC(transport *QUIC) uniConnOption {
	return func(conn *UniConn) {
		conn.connType = QUICType
		conn.rwc = transport
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
