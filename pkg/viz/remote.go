package viz

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

/*
RemoteBus forwards events over a WebSocket to a running viz server.
It reconnects automatically and buffers events during disconnects.
*/
type RemoteBus struct {
	url  string
	conn *websocket.Conn
	mu   sync.Mutex
	buf  []Event
}

/*
NewRemoteBus creates a remote forwarder targeting ws://addr/ws.
It does not connect until the first Forward or an explicit Connect call.
*/
func NewRemoteBus(addr string) *RemoteBus {
	return &RemoteBus{
		url: "ws://" + addr + "/ws",
		buf: make([]Event, 0, 256),
	}
}

/*
Connect attempts to establish the WebSocket connection.
*/
func (r *RemoteBus) Connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connectLocked()
}

func (r *RemoteBus) connectLocked() error {
	if r.conn != nil {
		return nil
	}

	conn, _, err := websocket.DefaultDialer.Dial(r.url, http.Header{})
	if err != nil {
		return err
	}

	r.conn = conn

	for _, ev := range r.buf {
		r.sendLocked(ev)
	}

	r.buf = r.buf[:0]
	return nil
}

func (r *RemoteBus) sendLocked(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	_ = r.conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))

	if err := r.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		r.conn.Close()
		r.conn = nil
	}
}

/*
Forward sends an event to the remote viz server.
*/
func (r *RemoteBus) Forward(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil {
		if err := r.connectLocked(); err != nil {
			if len(r.buf) < 4096 {
				r.buf = append(r.buf, ev)
			}
			return
		}
	}

	r.sendLocked(ev)
}

/*
Close shuts down the WebSocket connection.
*/
func (r *RemoteBus) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

/*
BridgeToRemote subscribes to a local Bus and forwards all events to a
remote viz server. Returns a cleanup function.
*/
func BridgeToRemote(local *Bus, addr string) (cleanup func()) {
	remote := NewRemoteBus(addr)

	if err := remote.Connect(); err != nil {
		log.Printf("viz: remote bridge to %s failed (will buffer): %v", addr, err)
	}

	ch := local.Subscribe(8192, nil)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}

				remote.Forward(ev)
			}
		}
	}()

	return func() {
		close(done)
		local.Unsubscribe(ch)
		remote.Close()
	}
}
