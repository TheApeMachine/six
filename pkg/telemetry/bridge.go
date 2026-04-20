package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gobwas/ws"
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
			// handle error
		}
		go func() {
			defer conn.Close()

			for {
				select {
				case <-bridge.ctx.Done():
					return
				default:
					header, err := ws.ReadHeader(conn)

					if err != nil {
						errnie.Error(errors.Join(
							io.ErrShortBuffer,
							errors.New("bridge.Write: ws.ReadHeader(conn) failed"),
						))
					}

					payload := make([]byte, header.Length)

					bridge.queue.Range(func(key, value any) bool {
						payload = append(payload, value.(*primitive.Value).Bytes()...)
						return true
					})

					if header.Masked {
						ws.Cipher(payload, header.Mask, 0)
					}

					// Reset the Masked flag, server frames must not be masked as
					// RFC6455 says.
					header.Masked = false

					if err := ws.WriteHeader(conn, header); err != nil {
						errnie.Error(errors.Join(
							io.ErrShortBuffer,
							errors.New("bridge.Write: ws.WriteHeader(conn, header) failed"),
						))
					}

					if _, err := conn.Write(payload); err != nil {
						errnie.Error(errors.Join(
							io.ErrShortBuffer,
							errors.New("bridge.Write: conn.Write(payload) failed"),
						))
					}

					if header.OpCode == ws.OpClose {
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
		return 0, io.ErrClosedPipe
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
