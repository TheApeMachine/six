package viz

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

//go:embed static/*
var staticFS embed.FS

// Server serves the 3D visualization UI and streams events over WebSocket.
type Server struct {
	bus      *Bus
	addr     string
	srv      *http.Server
	mu       sync.RWMutex
	clients  map[*websocket.Conn]context.CancelFunc
	timeline *Timeline
}

// NewServer creates a visualization server bound to the given address.
func NewServer(bus *Bus, addr string) *Server {
	if addr == "" {
		addr = ":6600"
	}

	s := &Server{
		bus:      bus,
		addr:     addr,
		clients:  make(map[*websocket.Conn]context.CancelFunc),
		timeline: NewTimeline(100_000), // ~100k events in ring buffer
	}

	mux := http.NewServeMux()

	// Serve static files from embedded FS.
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("viz: embedded static fs: %v", err))
	}

	mux.Handle("/", http.FileServer(http.FS(staticSub)))
	mux.Handle("/ws", websocket.Handler(s.handleWS))
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/prompt", s.handlePrompt)

	s.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

// Start activates the event bus, begins consuming events, and starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.bus.Activate()

	// Subscribe to all events.
	ch := s.bus.Subscribe(8192, nil)

	go s.consume(ctx, ch)

	log.Printf("viz: serving on http://localhost%s", s.addr)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
		s.bus.Unsubscribe(ch)
	}()

	if err := s.srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
}

// consume drains the event channel and fans out to WebSocket clients + timeline.
func (s *Server) consume(ctx context.Context, ch chan Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}

			// Record in timeline for scrubbing.
			s.timeline.Record(ev)

			// Broadcast to connected clients.
			s.broadcast(ev)
		}
	}
}

func (s *Server) broadcastExcept(ev Event, exclude *websocket.Conn) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for conn := range s.clients {
		if conn == exclude {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := conn.Write(data); err != nil {
			continue
		}
	}
}

func (s *Server) broadcast(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for conn := range s.clients {
		// Non-blocking write with small timeout.
		_ = conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := conn.Write(data); err != nil {
			// Will be cleaned up by the read loop detecting disconnect.
			continue
		}
	}
}

func (s *Server) handleWS(conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	s.clients[conn] = cancel
	clientCount := len(s.clients)
	s.mu.Unlock()

	log.Printf("viz: ws client connected (%d total)", clientCount)

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		cancel()
		conn.Close()
	}()

	// Send timeline history so new clients catch up.
	history := s.timeline.Range(0, s.timeline.Len())
	for _, ev := range history {
		data, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		if _, err := conn.Write(data); err != nil {
			return
		}
	}

	// Read loop: handle client commands (pause, scrub, prompt).
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := conn.Read(buf)

		if err != nil {
			return
		}

		// Try as a command first; if it has no "action" field, try as an
		// inbound event (from a remote BridgeToRemote publisher).
		var cmd ClientCommand
		if json.Unmarshal(buf[:n], &cmd) == nil && cmd.Action != "" {
			s.handleCommand(conn, cmd)
		} else {
			var ev Event
			if json.Unmarshal(buf[:n], &ev) == nil && ev.Timestamp != 0 {
				log.Printf("viz: inbound event kind=%d src=%s", ev.Kind, ev.Source)
				s.timeline.Record(ev)
				s.broadcastExcept(ev, conn)
			}
		}
	}
}

// ClientCommand is a message from the browser to the server.
type ClientCommand struct {
	Action string          `json:"action"` // "pause", "resume", "scrub", "prompt", "save", "load"
	Data   json.RawMessage `json:"data"`
}

func (s *Server) handleCommand(conn *websocket.Conn, cmd ClientCommand) {
	switch cmd.Action {
	case "scrub":
		var req struct {
			From int `json:"from"`
			To   int `json:"to"`
		}
		if json.Unmarshal(cmd.Data, &req) != nil {
			return
		}
		events := s.timeline.Range(req.From, req.To)
		resp, _ := json.Marshal(map[string]any{
			"action": "scrub_result",
			"events": events,
		})
		_, _ = conn.Write(resp)

	case "snapshot_save":
		data := s.timeline.Snapshot()
		resp, _ := json.Marshal(map[string]any{
			"action":   "snapshot_data",
			"snapshot": data,
		})
		_, _ = conn.Write(resp)
	}
}

// handleSnapshot returns a JSON snapshot of the full timeline.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data := s.timeline.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
		return
	}

	if r.Method == http.MethodPost {
		var events []Event
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		s.timeline.Load(events)
		w.WriteHeader(204)
		return
	}

	http.Error(w, "method not allowed", 405)
}

// promptHandler is set externally to inject prompts into the running system.
var promptHandler func(prompt string) (string, map[string]float64)

// SetPromptHandler installs the callback used when the viz UI sends a prompt.
func SetPromptHandler(fn func(string) (string, map[string]float64)) {
	promptHandler = fn
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if promptHandler == nil {
		http.Error(w, "no prompt handler configured", 503)
		return
	}

	generation, classification := promptHandler(req.Prompt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"generation":     generation,
		"classification": classification,
	})
}
