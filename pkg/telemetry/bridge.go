package telemetry

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/gobwas/ws"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Bridge serves exactly one read-only WebSocket client (the visualizer).

Bridge spawns no goroutines of its own. ListenAndServe blocks on the
caller's goroutine until Close is called or ctx is cancelled. Write emits
one binary WS frame per call, serialized by mu so concurrent producers do
not interleave headers and payloads on the wire.
*/
type Bridge struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	ln     net.Listener
	conn   net.Conn
}

func NewBridge(ctx context.Context, _ string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &Bridge{
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

/*
ListenAndServe accepts one connection on :6600, performs the WebSocket
handshake, and blocks until Close is called or ctx is cancelled. A closed
listener during shutdown returns nil; only setup failures bubble up.
*/
func (bridge *Bridge) ListenAndServe() error {
	if bridge.ln, bridge.err = net.Listen("tcp", ":6600"); bridge.err != nil {
		return bridge.err
	}

	if bridge.conn, bridge.err = bridge.ln.Accept(); bridge.err != nil {
		return bridge.err
	}

	if _, bridge.err = ws.Upgrade(bridge.conn); bridge.err != nil {
		bridge.conn.Close()
		return bridge.err
	}

	<-bridge.ctx.Done()

	return bridge.err
}

/*
Read is a noop.
*/
func (bridge *Bridge) Read(p []byte) (int, error) {
	return 0, io.EOF
}

/*
Write emits one binary WS frame. Before a client connects (or after the
client drops) Write reports the bytes as written and discards them, so
producers stay decoupled from visualizer presence.
*/
func (bridge *Bridge) Write(p []byte) (int, error) {
	if bridge == nil {
		return 0, io.ErrClosedPipe
	}

	if bridge.conn == nil {
		return 0, errnie.Error(errors.New("bridge not connected"))
	}

	select {
	case <-bridge.ctx.Done():
		return 0, bridge.ctx.Err()
	default:
		header := ws.Header{
			Fin:    true,
			OpCode: ws.OpBinary,
			Length: int64(len(p)),
		}

		if err := ws.WriteHeader(bridge.conn, header); err != nil {
			bridge.dropConnLocked()
			return 0, errnie.Error(err)
		}

		if _, err := bridge.conn.Write(p); err != nil {
			bridge.dropConnLocked()
			return 0, errnie.Error(err)
		}

		return len(p), nil
	}
}

func (bridge *Bridge) Close() error {
	if bridge == nil {
		return nil
	}

	bridge.cancel()

	if bridge.conn != nil {
		bridge.conn.Close()
		bridge.conn = nil
	}

	if bridge.ln != nil {
		bridge.ln.Close()
		bridge.ln = nil
	}

	return nil
}

func (bridge *Bridge) Error() error {
	return nil
}

func (bridge *Bridge) dropConnLocked() {
	if bridge.conn == nil {
		return
	}

	bridge.conn.Close()
	bridge.conn = nil
}
