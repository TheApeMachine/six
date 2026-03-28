package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"

	"golang.org/x/net/quic"
)

const quicReadyHandshakeByte = 0xA7

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
		return 0, &TransportError{Layer: "quic", Op: "read", Err: ErrQUICNoStream}
	}

	return q.stream.Read(p)
}

/*
Write sends bytes over the QUIC stream, flushing immediately so each
Value hits the wire as a distinct datagram when possible.
*/
func (q *QUIC) Write(p []byte) (int, error) {
	if q.stream == nil {
		return 0, &TransportError{Layer: "quic", Op: "write", Err: ErrQUICNoStream}
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
	if q.stream != nil {
		return nil
	}

	return q.accept(q.ctx)
}

func (q *QUIC) accept(ctx context.Context) error {
	if q.endpoint == nil {
		return &TransportError{Layer: "quic", Op: "accept", Err: ErrQUICNotListening}
	}

	if ctx == nil {
		ctx = q.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := q.endpoint.Accept(ctx)

	if err != nil {
		return err
	}

	stream, err := conn.AcceptStream(ctx)

	if err != nil {
		conn.Close()
		return err
	}

	if err := q.consumeHandshake(stream); err != nil {
		stream.Close()
		conn.Close()
		return err
	}

	q.conn = conn
	q.stream = stream

	return nil
}

// Ready normalizes transport readiness. Listener-side QUIC accepts the first
// connection+stream and consumes the internal handshake before reporting ready.
func (q *QUIC) Ready(ctx context.Context) error {
	if q.err != nil {
		return q.err
	}
	if q.stream != nil {
		return nil
	}
	if q.endpoint == nil {
		return &TransportError{Layer: "quic", Op: "ready", Err: ErrQUICNoStream}
	}
	return q.accept(ctx)
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

		if err := q.sendHandshake(stream); err != nil {
			stream.Close()
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
	ErrQUICHandshake    QUICError = "quic: handshake mismatch"
)

/*
Error implements the error interface for QUICError.
*/
func (quicErr QUICError) Error() string {
	return string(quicErr)
}

func (q *QUIC) sendHandshake(stream *quic.Stream) error {
	if stream == nil {
		return &TransportError{Layer: "quic", Op: "handshake_write", Err: ErrQUICNoStream}
	}
	if _, err := stream.Write([]byte{quicReadyHandshakeByte}); err != nil {
		return err
	}
	return stream.Flush()
}

func (q *QUIC) consumeHandshake(stream *quic.Stream) error {
	if stream == nil {
		return &TransportError{Layer: "quic", Op: "handshake_read", Err: ErrQUICNoStream}
	}

	var buf [1]byte
	if _, err := io.ReadFull(stream, buf[:]); err != nil {
		return err
	}
	if buf[0] != quicReadyHandshakeByte {
		return &TransportError{
			Layer: "quic",
			Op:    "handshake_read",
			Err:   fmt.Errorf("%w: got=0x%02x", ErrQUICHandshake, buf[0]),
		}
	}
	return nil
}
