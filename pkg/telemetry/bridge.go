package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type Bridge struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	conn   *websocket.Conn
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
	http.ListenAndServe(":6600", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

					bridge.queue.Range(func(_, v any) bool {
						payload = append(payload, v.(*primitive.Value).Bytes()...)
						bridge.queue.Delete(v.(*primitive.Value).ID())
						primitive.FreeValue(v.(*primitive.Value))
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

	value := primitive.AllocValue()

	if err := value.LoadFullFrame(p); err != nil {
		return 0, errnie.Error(errors.Join(
			io.ErrShortBuffer,
			fmt.Errorf("bridge.Write: value.LoadFullFrame(p) failed with size %d", len(p)),
		))
	}

	bridge.queue.Swap(value.ID(), value)

	return len(p), nil
}

func (bridge *Bridge) Error() error {
	if bridge == nil {
		return nil
	}

	return bridge.err
}
