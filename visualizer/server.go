package visualizer

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/api"
	"github.com/theapemachine/six/pool"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for dev
	},
}

/*
Visualizer is a decoupled actor that operates a WebSocket server to push
internal system states (`core.StateUpdate`) to a Three.js frontend.
It implements the `core.Updateable` interface so any component can
pass it a message, or it can be subscribed directly to the pool's
broadcast group.
*/
type Visualizer struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	addr    string
	pool    *pool.Pool
	updates chan map[string]any
}

func NewVisualizer(addr string, p *pool.Pool) *Visualizer {
	return &Visualizer{
		clients: make(map[*websocket.Conn]bool),
		addr:    addr,
		pool:    p,
		updates: make(chan map[string]any, 1000), // Buffered channel for backpressure
	}
}

/*
Start spins up the static file server and the WebSocket handler.
It also starts the background broadcaster that pumps state updates
to all connected clients.
*/
func (v *Visualizer) Start() {
	go v.broadcaster()

	go func() {
		// Replace standard mux to allow clean routing without global conflicts
		mux := http.NewServeMux()
		
		// Serve the three.js visualization files
		fs := http.FileServer(http.Dir("./visualizer/static"))
		mux.Handle("/", fs)

		// WebSocket endpoint for the stream
		mux.HandleFunc("/ws", v.wsHandler)

		log.Infof("Visualizer starting on http://localhost%s", v.addr)
		if err := http.ListenAndServe(v.addr, mux); err != nil {
			log.Errorf("Visualizer server failed: %v", err)
		}
	}()
}

/*
UpdateStream implements the Cap'n Proto api.Telemetry_Server interface.
It handles streaming binary RPC connections.
*/
func (v *Visualizer) UpdateStream(ctx context.Context, call api.Telemetry_updateStream) error {
	params := call.Args()
	msg, err := params.Update()
	if err != nil {
		return err
	}

	comp, _ := msg.Component()
	act, _ := msg.Action()

	payload := map[string]any{
		"component": comp,
		"action":    act,
		"data": map[string]any{
			"residue": msg.Residue(),
			"paths":   msg.Paths(),
			"edges":   msg.Edges(),
		},
	}

	select {
	case v.updates <- payload:
	default:
		log.Warn("Visualizer channel full, dropping frame.")
	}
	return nil
}

func (v *Visualizer) Done(ctx context.Context, call api.Telemetry_done) error {
	return nil
}

func (v *Visualizer) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("WebSocket upgrade failed: %v", err)
		return
	}

	v.mu.Lock()
	v.clients[conn] = true
	v.mu.Unlock()

	log.Infof("New visualizer client connected from %s", r.RemoteAddr)

	// Keep connection open and handle closure
	go func() {
		defer func() {
			v.mu.Lock()
			delete(v.clients, conn)
			v.mu.Unlock()
			conn.Close()
			log.Infof("Visualizer client disconnected")
		}()
		for {
			// Read messages (ping/pong check, mostly to detect disconnects)
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

// broadcaster reads from the internal queue and sends concurrent writes
// to all active Three.js clients.
func (v *Visualizer) broadcaster() {
	for update := range v.updates {
		payload, err := json.Marshal(update)
		if err != nil {
			continue
		}

		v.mu.RLock()
		for client := range v.clients {
			// Set a short write deadline to prevent a slow client from blocking others
			client.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
				client.Close()
				delete(v.clients, client) // Clean up dead connections
			}
		}
		v.mu.RUnlock()
	}
}
