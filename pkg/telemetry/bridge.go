package telemetry

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Bridge is the runtime uplink for raw primitive.Value wire frames.

The browser and this process both connect as WebSocket clients to the same
hub (visualizer/server/bridge.ts on :6600). In development, Vite proxies
/ws on the dev server port to that hub so config can use ws://127.0.0.1:3000/ws.

This type implements io.Writer: each Write sends one binary message on the
client connection (after connect succeeds). Reconnects with backoff when the
hub or proxy is not up yet.
*/
type Bridge struct {
	ctx    context.Context
	cancel context.CancelFunc
	url    string
	send   chan []byte
}

func NewBridge(ctx context.Context, url string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	trimmed := strings.TrimSpace(url)

	return &Bridge{
		ctx:    ctx,
		cancel: cancel,
		url:    trimmed,
		send:   make(chan []byte, 8192),
	}, nil
}

/*
Connect runs until the bridge context is cancelled. When url is empty, it
returns immediately (tests and runs that disable telemetry).

When url is set, it dials in a loop and forwards frames from send onto the
socket until Close/cancel.
*/
func (bridge *Bridge) Connect() error {
	if bridge == nil {
		return errnie.Error(errors.New("bridge is nil"))
	}

	if bridge.url == "" {
		return nil
	}

	bridge.connectLoop()
	return bridge.ctx.Err()
}

func (bridge *Bridge) connectLoop() {
	backoff := 200 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		if bridge.ctx.Err() != nil {
			return
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}

		conn, _, dialErr := dialer.DialContext(bridge.ctx, bridge.url, nil)
		if dialErr != nil {
			errnie.Trace("telemetry.Bridge.Connect: dial", dialErr.Error())

			select {
			case <-bridge.ctx.Done():
				return
			case <-time.After(backoff):
			}

			if backoff < maxBackoff {
				backoff *= 2
			}

			continue
		}

		backoff = 200 * time.Millisecond

		writeErr := bridge.pump(conn)
		_ = conn.Close()

		if bridge.ctx.Err() != nil {
			return
		}

		if writeErr != nil && !errors.Is(writeErr, context.Canceled) {
			errnie.Trace("telemetry.Bridge.Connect: pump", writeErr.Error())
		}

		select {
		case <-bridge.ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (bridge *Bridge) pump(conn *websocket.Conn) error {
	for {
		select {
		case <-bridge.ctx.Done():
			return bridge.ctx.Err()

		case payload := <-bridge.send:
			deadline := time.Now().Add(15 * time.Second)

			if err := conn.SetWriteDeadline(deadline); err != nil {
				return err
			}

			if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				return err
			}
		}
	}
}

func (bridge *Bridge) Close() error {
	if bridge == nil {
		return nil
	}

	bridge.cancel()

	return nil
}

/*
Read is a no-op for the bridge.
*/
func (bridge *Bridge) Read(p []byte) (int, error) {
	errnie.Trace("telemetry.Bridge.Read")

	return 0, io.EOF
}

func (bridge *Bridge) Write(p []byte) (int, error) {
	errnie.Trace("telemetry.Bridge.Write")

	if bridge == nil {
		return 0, errnie.Error(io.ErrClosedPipe, errors.New("bridge is nil"))
	}

	if bridge.url == "" {
		return len(p), nil
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	select {
	case <-bridge.ctx.Done():
		return 0, bridge.ctx.Err()
	case bridge.send <- buf:
		return len(p), nil
	default:
		// Drop the telemetry frame if the buffer is full (e.g. no connection)
		return len(p), nil
	}
}
