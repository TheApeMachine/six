package telemetry

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

const bridgeDialHandshake = 10 * time.Second
const bridgeWriteDeadline = 15 * time.Second
const bridgeMaxBackoff = 5 * time.Second
const bridgeInitialBackoff = 200 * time.Millisecond
const bridgeHashOffset64 uint64 = 14695981039346656037
const bridgeHashPrime64 uint64 = 1099511628211

/*
Bridge is the runtime uplink for raw primitive.Value wire frames.

The browser and this process both connect as WebSocket clients to the same
hub (visualizer/server/bridge.ts on :6600). In development, Vite proxies
/ws on the dev server port to that hub so config can use ws://127.0.0.1:3000/ws.

This type implements io.Writer: each Write accepts one or more raw Value
frames, filters out frames whose bytes match the last successfully sent image
for that Value ID, and sends the changed frames as one binary message on a
dedicated client connection. The flow is connect (dial, held until Close),
write changed frames, and disconnect only on Close, transport error, or context
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
	sent    map[uint64]uint64
}

type bridgeFrameFingerprint struct {
	key  uint64
	hash uint64
}

func NewBridge(ctx context.Context, url string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	trimmed := strings.TrimSpace(url)

	return &Bridge{
		ctx:     ctx,
		cancel:  cancel,
		url:     trimmed,
		backoff: bridgeInitialBackoff,
		sent:    make(map[uint64]uint64),
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

	bridge.connMu.Lock()
	defer bridge.connMu.Unlock()

	buf, fingerprints := bridge.changedPayloadLocked(p)
	if len(fingerprints) == 0 {
		return len(p), nil
	}

	if bridge.ctx.Err() != nil {
		return 0, bridge.ctx.Err()
	}

	if bridge.url == "" {
		bridge.commitFingerprintsLocked(fingerprints)

		return len(p), nil
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

		return len(p), nil
	}

	bridge.commitFingerprintsLocked(fingerprints)

	return len(p), nil
}

func (bridge *Bridge) changedPayloadLocked(p []byte) ([]byte, []bridgeFrameFingerprint) {
	if len(p) == 0 {
		return nil, nil
	}

	if len(p) >= primitive.FrameByteLength && len(p)%primitive.FrameByteLength == 0 {
		return bridge.changedFramesLocked(p)
	}

	fingerprint := bridge.fingerprint(p)
	if bridge.sent[fingerprint.key] == fingerprint.hash {
		return nil, nil
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	return buf, []bridgeFrameFingerprint{fingerprint}
}

func (bridge *Bridge) changedFramesLocked(p []byte) ([]byte, []bridgeFrameFingerprint) {
	fingerprints := make([]bridgeFrameFingerprint, 0, len(p)/primitive.FrameByteLength)
	buf := make([]byte, 0, len(p))

	for start := 0; start < len(p); start += primitive.FrameByteLength {
		frame := p[start : start+primitive.FrameByteLength]
		fingerprint := bridge.fingerprint(frame)
		if bridge.sent[fingerprint.key] == fingerprint.hash {
			continue
		}

		buf = append(buf, frame...)
		fingerprints = append(fingerprints, fingerprint)
	}

	return buf, fingerprints
}

func (bridge *Bridge) fingerprint(p []byte) bridgeFrameFingerprint {
	hash := bridgeFrameHash(p)
	key := hash

	idOffset := primitive.IDStartWord * 8
	if len(p) >= idOffset+8 {
		if id := binary.LittleEndian.Uint64(p[idOffset:]); id != 0 {
			key = id
		}
	}

	return bridgeFrameFingerprint{
		key:  key,
		hash: hash,
	}
}

func (bridge *Bridge) commitFingerprintsLocked(fingerprints []bridgeFrameFingerprint) {
	if bridge.sent == nil {
		bridge.sent = make(map[uint64]uint64)
	}

	for _, fingerprint := range fingerprints {
		bridge.sent[fingerprint.key] = fingerprint.hash
	}
}

func bridgeFrameHash(p []byte) uint64 {
	hash := uint64(bridgeHashOffset64)
	for _, b := range p {
		hash ^= uint64(b)
		hash *= bridgeHashPrime64
	}

	hash ^= uint64(len(p))
	hash *= bridgeHashPrime64

	return hash
}

func (bridge *Bridge) scheduleBackoffAfterFailure(now time.Time) {
	bridge.cool = now.Add(bridge.backoff)

	if bridge.backoff < bridgeMaxBackoff {
		bridge.backoff *= 2
	}
}
