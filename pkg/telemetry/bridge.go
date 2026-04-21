package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

type Bridge struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	url    string
	seq    atomic.Uint64
	queue  sync.Map
}

func NewBridge(ctx context.Context, url string) (*Bridge, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &Bridge{
		ctx:    ctx,
		cancel: cancel,
		url:    url,
		queue:  sync.Map{},
	}, nil
}

func (bridge *Bridge) ListenAndServe() error {
	go func() {
		if bridge.url == "" {
			return
		}

		for {
			select {
			case <-bridge.ctx.Done():
				return
			default:
				// Try to connect to the bridge
				conn, _, err := websocket.DefaultDialer.DialContext(bridge.ctx, bridge.url, nil)
				if err != nil {
					// If we can't connect, just clear the queue and wait a bit
					bridge.queue.Range(func(k, v any) bool {
						bridge.queue.Delete(k)
						return true
					})
					// Sleep a bit before retrying
					select {
					case <-bridge.ctx.Done():
						return
					case <-time.After(1 * time.Second):
					}
					continue
				}

				// Connected, now send messages from the queue
				for {
					select {
					case <-bridge.ctx.Done():
						conn.Close()
						return
					default:
						var payload []byte

						bridge.queue.Range(func(k, v any) bool {
							payload = append(payload, v.([]byte)...)
							bridge.queue.Delete(k)
							return true
						})

						if len(payload) == 0 {
							// Sleep a bit to avoid tight loop
							time.Sleep(10 * time.Millisecond)
							continue
						}

						if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
							errnie.Error(errors.Join(
								err,
								fmt.Errorf("conn.WriteMessage(websocket.BinaryMessage, payload) failed"),
							))
							conn.Close()
							goto reconnect
						}
					}
				}
			reconnect:
			}
		}
	}()

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
