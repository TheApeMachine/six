package telemetry

import (
	"context"
	"io"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Client is a WebSocket client to the visualizer bridge. Write sends binary
messages directly to the socket so frames reach the TypeScript bridge as
soon as gossip.Conn writes them (no pipe or TeeReader drain required).
*/
type Client struct {
	ctx     context.Context
	cancel  context.CancelFunc
	conn    *websocket.Conn
	writeMu sync.Mutex
}

/*
NewClient dials the bridge WebSocket URL. Returns nil when dial fails.
*/
func NewClient(ctx context.Context, url string) *Client {
	ctx, cancel := context.WithCancel(ctx)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)

	if err != nil {
		cancel()

		return nil
	}

	return &Client{
		ctx:    ctx,
		cancel: cancel,
		conn:   conn,
	}
}

/*
Write sends one binary WebSocket message per call. The mutex serializes
writes so MultiWriter and TeeReader paths do not violate gorilla/websocket's
one-writer rule.
*/
func (client *Client) Write(p []byte) (int, error) {
	if client == nil || client.conn == nil {
		return 0, io.ErrClosedPipe
	}

	client.writeMu.Lock()
	defer client.writeMu.Unlock()

	if err := client.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, errnie.Error(err)
	}

	return len(p), nil
}

/*
Read is unused on the producer path. Returns io.EOF so callers implementing
io.ReadWriter do not block; server-to-client traffic would use ReadMessage
in a dedicated loop if needed later.
*/
func (client *Client) Read(p []byte) (int, error) {
	if client == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) == 0 {
		return 0, nil
	}

	return 0, io.EOF
}

/*
Close releases the context and closes the WebSocket.
*/
func (client *Client) Close() error {
	if client == nil {
		return nil
	}

	client.cancel()

	if client.conn == nil {
		return nil
	}

	err := client.conn.Close()
	client.conn = nil

	return errnie.Error(err)
}
