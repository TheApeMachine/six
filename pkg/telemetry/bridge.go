package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

type Bridge struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	conn   *websocket.Conn
	seq    atomic.Uint64
	queue  sync.Map
}

func NewBridge(ctx context.Context, url string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &Bridge{
		ctx:    ctx,
		cancel: cancel,
		queue:  sync.Map{},
	}, nil
}

func (bridge *Bridge) ListenAndServe() error {
	err := http.ListenAndServe(":6600", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			errnie.Error(errors.Join(
				err,
				fmt.Errorf("ws.UpgradeHTTP(r, w) failed"),
			))

			return
		}
		go func() {
			for {
				select {
				case <-bridge.ctx.Done():
					return
				default:
					var payload []byte

					bridge.queue.Range(func(k, v any) bool {
						payload = append(payload, v.([]byte)...)
						bridge.queue.Delete(k)
						return true
					})

					if len(payload) == 0 {
						continue
					}

					if err := wsutil.WriteServerMessage(
						conn, websocket.BinaryMessage, payload,
					); err != nil {
						errnie.Error(errors.Join(
							err,
							fmt.Errorf("wsutil.WriteServerMessage(conn, websocket.BinaryMessage, payload) failed"),
						))
						return
					}
				}
			}
		}()
	}))

	if err != nil {
		return errnie.Error(err)
	}

	return nil
}

func (bridge *Bridge) Close() error {
	if bridge == nil {
		return nil
	}

	bridge.cancel()

	return bridge.err
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

	buf := make([]byte, len(p))
	copy(buf, p)
	bridge.queue.Store(bridge.seq.Add(1), buf)

	return len(p), nil
}

func (bridge *Bridge) Error() error {
	if bridge == nil {
		return nil
	}

	return bridge.err
}
