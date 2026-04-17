package telemetry

import (
	"context"
	"io"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
websocketBinaryWriter adapts *websocket.Conn to io.Writer for io.TeeReader.
Each Write becomes one binary WebSocket message.
*/
type websocketBinaryWriter struct {
	conn *websocket.Conn
}

func (writer *websocketBinaryWriter) Write(p []byte) (int, error) {
	if err := writer.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, errnie.Error(err)
	}

	return len(p), nil
}

/*
Client is a WebSocket client to the visualizer bridge. Write pushes bytes
into a pipe; Read pulls from the pipe while io.TeeReader sends a duplicate
of each chunk to the bridge as a binary WebSocket message.
*/
type Client struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
	url    string
	tee    io.Reader
	pw     *io.PipeWriter
	pr     *io.PipeReader
	conn   *websocket.Conn
}

/*
NewClient constructs a client.
*/
func NewClient(ctx context.Context, url string) *Client {
	ctx, cancel := context.WithCancel(ctx)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)

	if err != nil {
		cancel()

		return nil
	}

	pr, pw := io.Pipe()
	tee := io.TeeReader(pr, &websocketBinaryWriter{conn: conn})

	return &Client{
		ctx:    ctx,
		cancel: cancel,
		url:    url,
		conn:   conn,
		pw:     pw,
		pr:     pr,
		tee:    tee,
	}
}

/*
Write sends bytes into the pipe; a matching Read drains them and mirrors
each chunk to the bridge as a binary WebSocket message.
*/
func (client *Client) Write(p []byte) (int, error) {
	return client.pw.Write(p)
}

/*
Read copies the next chunk from the pipe read side; each chunk is also sent
to the bridge via the tee.
*/
func (client *Client) Read(p []byte) (int, error) {
	n, err := client.tee.Read(p)

	return n, errnie.Error(err)
}

/*
Close releases the context, closes the pipe writer, and closes the WebSocket.
*/
func (client *Client) Close() error {
	client.cancel()

	err := client.pw.Close()

	if cerr := client.conn.Close(); cerr != nil && err == nil {
		err = cerr
	}

	return err
}
