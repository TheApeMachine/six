package network

import (
	"context"
	"crypto/tls"

	"golang.org/x/net/quic"
)

/*
QUIC provides reliable WAN transport using golang.org/x/net/quic.
A single bidirectional stream carries Value frames, giving
io.ReadWriteCloser semantics with the congestion control and
encryption that raw UDP lacks.
*/
type QUIC struct {
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	endpoint *quic.Endpoint
	conn     *quic.Conn
	stream   *quic.Stream
}

/*
quicOption configures a QUIC transport at construction time.
*/
type quicOption func(*QUIC)

/*
NewQUIC constructs a QUIC transport. Use QUICWithListen to start
an endpoint (then call Accept), or QUICWithDial to connect outbound,
or QUICWithStream to wrap an already-accepted stream.
*/
func NewQUIC(opts ...quicOption) *QUIC {
	ctx, cancel := context.WithCancel(context.Background())

	q := &QUIC{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(q)
	}

	return q
}

/*
Read receives bytes from the QUIC stream.
*/
func (q *QUIC) Read(p []byte) (int, error) {
	if q.stream == nil {
		return 0, ErrQUICNoStream
	}

	return q.stream.Read(p)
}

/*
Write sends bytes over the QUIC stream, flushing immediately so each
Value hits the wire as a distinct datagram when possible.
*/
func (q *QUIC) Write(p []byte) (int, error) {
	if q.stream == nil {
		return 0, ErrQUICNoStream
	}

	n, err := q.stream.Write(p)

	if err != nil {
		return n, err
	}

	return n, q.stream.Flush()
}

/*
Close tears down the stream, connection, and endpoint in order.
Resources not owned by this instance (e.g. when constructed via
QUICWithStream) are left untouched.
*/
func (q *QUIC) Close() error {
	q.cancel()
	var firstErr error

	if q.stream != nil {
		if err := q.stream.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if q.conn != nil {
		if err := q.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if q.endpoint != nil {
		if err := q.endpoint.Close(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

/*
Accept blocks until an inbound connection arrives on the endpoint
created by QUICWithListen, then opens the first bidirectional stream.
*/
func (q *QUIC) Accept() error {
	if q.endpoint == nil {
		return ErrQUICNotListening
	}

	conn, err := q.endpoint.Accept(q.ctx)

	if err != nil {
		return err
	}

	stream, err := conn.AcceptStream(q.ctx)

	if err != nil {
		conn.Close()
		return err
	}

	q.conn = conn
	q.stream = stream

	return nil
}

/*
QUICWithListen creates a QUIC endpoint listening on addr. Call Accept
separately to wait for an inbound connection and stream.
*/
func QUICWithListen(addr string, tlsConf *tls.Config) quicOption {
	return func(q *QUIC) {
		endpoint, err := quic.Listen("udp", addr, &quic.Config{
			TLSConfig: tlsConf,
		})

		if err != nil {
			q.err = err
			return
		}

		q.endpoint = endpoint
	}
}

/*
QUICWithDial connects to a remote QUIC endpoint and opens a
bidirectional stream. The transport is ready for Read/Write
immediately after construction.
*/
func QUICWithDial(addr string, tlsConf *tls.Config) quicOption {
	return func(q *QUIC) {
		endpoint, err := quic.Listen("udp", ":0", nil)

		if err != nil {
			q.err = err
			return
		}

		conn, err := endpoint.Dial(q.ctx, "udp", addr, &quic.Config{
			TLSConfig: tlsConf,
		})

		if err != nil {
			endpoint.Close(context.Background())
			q.err = err
			return
		}

		stream, err := conn.NewStream(q.ctx)

		if err != nil {
			conn.Close()
			endpoint.Close(context.Background())
			q.err = err
			return
		}

		q.endpoint = endpoint
		q.conn = conn
		q.stream = stream
	}
}

/*
QUICWithStream wraps an already-accepted *quic.Stream. Use this on
the server side when an external accept loop manages the endpoint
and connection lifecycle.
*/
func QUICWithStream(stream *quic.Stream) quicOption {
	return func(q *QUIC) {
		q.stream = stream
	}
}

/*
QUICWithContext replaces the default background context, which
controls Accept and Dial cancellation.
*/
func QUICWithContext(ctx context.Context) quicOption {
	return func(q *QUIC) {
		q.cancel()
		q.ctx, q.cancel = context.WithCancel(ctx)
	}
}

/*
QUICError is a typed error for QUIC transport failures.
*/
type QUICError string

const (
	ErrQUICNoStream     QUICError = "quic: no active stream"
	ErrQUICNotListening QUICError = "quic: no endpoint listening"
)

/*
Error implements the error interface for QUICError.
*/
func (quicErr QUICError) Error() string {
	return string(quicErr)
}
