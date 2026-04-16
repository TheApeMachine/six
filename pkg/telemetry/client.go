package telemetry

import (
	"context"
	"io"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Client is a WebSocket client to the visualizer bridge. Each Write sends one
binary WebSocket message so io.Copy, io.TeeReader, and io.MultiWriter can
fan out Value frames without blocking on a pipe (the previous pipe+tee
design deadlocked when nothing drained Read while MultiWriter wrote).
*/
type Client struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	url    string
	conn   *websocket.Conn
}

/*
NewClient constructs a client and dials the bridge.
*/
func NewClient(ctx context.Context, url string) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)

	if err != nil {
		cancel()

		return nil, errnie.Error(err)
	}

	return &Client{
			ctx:    ctx,
			cancel: cancel,
			url:    url,
			conn:   conn,
		}, validate.Require(map[string]any{
			"ctx":    ctx,
			"cancel": cancel,
			"url":    url,
			"conn":   conn,
		})
}

/*
Write sends one binary WebSocket frame (one message per call).
*/
func (client *Client) Write(p []byte) (int, error) {
	if err := client.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, errnie.Error(err)
	}

	return len(p), nil
}

/*
Read is not used on the outbound path; it exists so *Client may satisfy
io.ReadWriteCloser where callers embed it in interfaces.
*/
func (client *Client) Read(p []byte) (int, error) {
	return 0, io.EOF
}

/*
Close releases the context and closes the WebSocket.
*/
func (client *Client) Close() error {
	client.cancel()

	if cerr := client.conn.Close(); cerr != nil {
		return errnie.Error(cerr)
	}

	return nil
}
