package telemetry

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

const bridgeDialHandshake = 10 * time.Second
const bridgeWriteDeadline = 15 * time.Second
const bridgeMaxBackoff = 5 * time.Second
const bridgeInitialBackoff = 200 * time.Millisecond

/*
Bridge is the runtime uplink for raw primitive.Value wire frames.

The browser and this process both connect as WebSocket clients to the same
hub (visualizer/server/bridge.ts on :6600). In development, Vite proxies
/ws on the dev server port to that hub so config can use ws://127.0.0.1:3000/ws.

This type implements io.Writer: each Write sends one binary message on a
dedicated client connection. The flow is connect (dial, held until Close),
write the frame, and disconnect only on Close, transport error, or context
cancel — there is no background pump goroutine, so nothing is left running
invisibly.
*/
type Bridge struct {
	ctx     context.Context
	cancel  context.CancelFunc
	url     string
	connMu  sync.Mutex
	conn    *websocket.Conn
	cool    time.Time
	backoff time.Duration
}

func NewBridge(ctx context.Context, url string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	trimmed := strings.TrimSpace(url)

	return &Bridge{
		ctx:     ctx,
		cancel:  cancel,
		url:     trimmed,
		backoff: bridgeInitialBackoff,
	}, nil
}

/*
Connect blocks until a WebSocket to url succeeds, the bridge context is
cancelled, or url is empty (tests and runs that disable telemetry). Use it
to wait for the hub before the hot path; otherwise the first Write dials
with the same connect / write / disconnect lifecycle.
*/
func (bridge *Bridge) Connect() error {
	if bridge == nil {
		return errnie.Error(errors.New("bridge is nil"))
	}

	if bridge.url == "" {
		return nil
	}

	retry := bridgeInitialBackoff

	for {
		if bridge.ctx.Err() != nil {
			return bridge.ctx.Err()
		}

		bridge.connMu.Lock()

		if bridge.conn != nil {
			bridge.connMu.Unlock()

			return nil
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: bridgeDialHandshake,
		}

		conn, _, dialErr := dialer.DialContext(bridge.ctx, bridge.url, nil)
		if dialErr != nil {
			bridge.connMu.Unlock()
			errnie.Trace("telemetry.Bridge.Connect: dial", dialErr.Error())

			select {
			case <-bridge.ctx.Done():
				return bridge.ctx.Err()
			case <-time.After(retry):
			}

			if retry < bridgeMaxBackoff {
				retry *= 2
			}

			continue
		}

		bridge.conn = conn
		bridge.cool = time.Time{}
		bridge.backoff = bridgeInitialBackoff
		bridge.connMu.Unlock()

		return nil
	}
}

func (bridge *Bridge) connectLocked() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: bridgeDialHandshake,
	}

	conn, _, err := dialer.DialContext(bridge.ctx, bridge.url, nil)
	if err != nil {
		return err
	}

	bridge.conn = conn
	bridge.cool = time.Time{}
	bridge.backoff = bridgeInitialBackoff

	return nil
}

func (bridge *Bridge) Close() error {
	if bridge == nil {
		return nil
	}

	bridge.cancel()
	bridge.connMu.Lock()

	if bridge.conn != nil {
		_ = bridge.conn.Close()
		bridge.conn = nil
	}

	bridge.connMu.Unlock()

	return nil
}

/*
Read is a no-op for the bridge.
*/
func (bridge *Bridge) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (bridge *Bridge) Write(p []byte) (int, error) {
	if bridge == nil {
		return 0, errnie.Error(io.ErrClosedPipe, errors.New("bridge is nil"))
	}

	if bridge.url == "" {
		return len(p), nil
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	bridge.connMu.Lock()
	defer bridge.connMu.Unlock()

	if bridge.ctx.Err() != nil {
		return 0, bridge.ctx.Err()
	}

	now := time.Now()

	if bridge.conn == nil {
		if !bridge.cool.IsZero() && now.Before(bridge.cool) {
			return len(p), nil
		}

		if err := bridge.connectLocked(); err != nil {
			bridge.scheduleBackoffAfterFailure(now)
			errnie.Trace("telemetry.Bridge.Write: dial", err.Error())

			return len(p), nil
		}
	}

	deadline := time.Now().Add(bridgeWriteDeadline)

	if err := bridge.conn.SetWriteDeadline(deadline); err != nil {
		_ = bridge.conn.Close()
		bridge.conn = nil
		bridge.scheduleBackoffAfterFailure(now)

		return len(p), nil
	}

	if err := bridge.conn.WriteMessage(websocket.BinaryMessage, buf); err != nil {
		_ = bridge.conn.Close()
		bridge.conn = nil
		bridge.scheduleBackoffAfterFailure(now)
		errnie.Trace("telemetry.Bridge.Write: message", err.Error())
	}

	return len(p), nil
}

func (bridge *Bridge) scheduleBackoffAfterFailure(now time.Time) {
	bridge.cool = now.Add(bridge.backoff)

	if bridge.backoff < bridgeMaxBackoff {
		bridge.backoff *= 2
	}
}
