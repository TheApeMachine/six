package viz

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static/*
var staticFS embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

/*
wsClient wraps a gorilla websocket connection with a serialized write
channel. All writes go through the outbound channel so the single
writer goroutine is the only thing touching conn.WriteMessage.
*/
type wsClient struct {
	conn     *websocket.Conn
	outbound chan []byte
	cancel   context.CancelFunc
}

/*
Server serves the 3D visualization UI and streams events over WebSocket.
*/
type Server struct {
	bus      *Bus
	addr     string
	srv      *http.Server
	ln       net.Listener
	ch       chan Event
	mu       sync.RWMutex
	clients  map[*wsClient]struct{}
	timeline *Timeline
}

/*
NewServer creates a visualization server bound to the given address.
*/
func NewServer(bus *Bus, addr string) *Server {
	if addr == "" {
		addr = ":6600"
	}

	s := &Server{
		bus:      bus,
		addr:     addr,
		clients:  make(map[*wsClient]struct{}),
		timeline: NewTimeline(100_000),
	}

	mux := http.NewServeMux()

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("viz: embedded static fs: %v", err))
	}

	mux.Handle("/", http.FileServer(http.FS(staticSub)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/prompt", s.handlePrompt)

	s.srv = &http.Server{
		Handler: mux,
	}

	return s
}

/*
Start activates the event bus, begins consuming events, and starts the
HTTP server. This is the all-in-one entry point — blocks until the
server shuts down.
*/
func (s *Server) Start(ctx context.Context) error {
	if err := s.ListenAndActivate(); err != nil {
		return err
	}

	return s.Serve()
}

/*
ListenAndActivate binds the TCP listener, activates the bus, and
subscribes the event consumer — all synchronously. After this returns,
any Publish call will be captured. Call Serve separately to start
accepting HTTP connections.
*/
func (s *Server) ListenAndActivate() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.addr = ln.Addr().String()
	s.ln = ln
	s.bus.Activate()
	s.ch = s.bus.Subscribe(8192, nil)

	log.Printf("viz: serving on http://%s", s.addr)

	return nil
}

/*
Serve starts the consume loop, stats broadcaster, and HTTP server on
the listener opened by ListenAndActivate. Blocks until shutdown.
*/
func (s *Server) Serve() error {
	ctx := context.Background()

	go s.consume(ctx, s.ch)
	go s.broadcastStats(ctx)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
		s.bus.Unsubscribe(s.ch)
	}()

	if err := s.srv.Serve(s.ln); err != http.ErrServerClosed {
		return err
	}

	return nil
}

/*
consume drains the event channel and fans out to WebSocket clients + timeline.
*/
func (s *Server) consume(ctx context.Context, ch chan Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}

			s.timeline.Record(ev)
			s.broadcastEvent(ev)
		}
	}
}

/*
broadcastStats periodically sends bus statistics to all connected clients
so the UI can display dropped event counts accurately.
*/
func (s *Server) broadcastStats(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.send(MarshalWireStats(s.bus.Dropped()), nil)
		}
	}
}

/*
send pushes a pre-serialized message to all connected clients (or all
except `exclude`). Non-blocking: if a client's outbound channel is full
the message is dropped for that client.
*/
func (s *Server) send(data []byte, exclude *wsClient) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.clients {
		if client == exclude {
			continue
		}

		select {
		case client.outbound <- data:
		default:
		}
	}
}

func (s *Server) broadcastEvent(ev Event) {
	s.send(MarshalWireEvent(ev), nil)
}

func (s *Server) broadcastExcept(ev Event, exclude *wsClient) {
	s.send(MarshalWireEvent(ev), exclude)
}

/*
writeLoop is the single goroutine that owns writes to a connection.
All outbound data is funneled through client.outbound so gorilla
never sees concurrent WriteMessage calls. Viz data uses binary frames; pings stay WS control.
*/
func (s *Server) writeLoop(ctx context.Context, client *wsClient) {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-client.outbound:
			if !ok {
				return
			}

			_ = client.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			if err := client.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				return
			}

		case <-pingTicker.C:
			_ = client.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("viz: ws upgrade failed: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &wsClient{
		conn:     conn,
		outbound: make(chan []byte, 65536),
		cancel:   cancel,
	}

	s.mu.Lock()
	s.clients[client] = struct{}{}
	clientCount := len(s.clients)
	s.mu.Unlock()

	log.Printf("viz: ws client connected (%d total)", clientCount)

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		cancel()
		conn.Close()
	}()

	// Start the serialized write loop.
	go s.writeLoop(ctx, client)

	history := s.timeline.Range(0, s.timeline.Len())

	// One synthetic control message so the client can materialize nodes even
	// when the retained window no longer contains NodeCreated / PeerAdded.
	nodeSeen := make(map[string]struct{})

	for _, ev := range history {
		if strings.HasPrefix(ev.Source, "node_") {
			nodeSeen[ev.Source] = struct{}{}
		}

		if strings.HasPrefix(ev.Target, "node_") {
			nodeSeen[ev.Target] = struct{}{}
		}
	}

	nodeIDs := make([]string, 0, len(nodeSeen))

	for id := range nodeSeen {
		nodeIDs = append(nodeIDs, id)
	}

	sort.Strings(nodeIDs)

	// Blocking: a non-blocking send drops the entire replay when the buffer
	// cannot absorb bootstrap + history in one scheduler quantum (common for
	// paper-sized timelines or late-joining browsers).
	client.outbound <- MarshalWireBootstrap(nodeIDs)

	// Send timeline history so new clients catch up (same blocking guarantee).
	for _, ev := range history {
		client.outbound <- MarshalWireEvent(ev)
	}

	// Read loop: handle client commands.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if ev, ok := TryUnmarshalWireEvent(message); ok {
			log.Printf("viz: inbound event kind=%d src=%s", ev.Kind, ev.Source)
			s.timeline.Record(ev)
			s.broadcastExcept(ev, client)
			continue
		}

		var cmd ClientCommand
		if json.Unmarshal(message, &cmd) == nil && cmd.Action != "" {
			s.handleCommand(client, cmd)
			continue
		}

		var ev Event
		if json.Unmarshal(message, &ev) == nil && ev.Timestamp != 0 {
			log.Printf("viz: inbound event kind=%d src=%s", ev.Kind, ev.Source)
			s.timeline.Record(ev)
			s.broadcastExcept(ev, client)
		}
	}
}

/*
ClientCommand is a message from the browser to the server.
*/
type ClientCommand struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

func (s *Server) handleCommand(client *wsClient, cmd ClientCommand) {
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
		client.outbound <- MarshalWireScrub(events)

	case "snapshot_save":
		data := s.timeline.Snapshot()
		resp, err := json.Marshal(map[string]any{
			"action":   "snapshot_data",
			"snapshot": data,
		})
		if err != nil {
			return
		}

		select {
		case client.outbound <- MarshalWireJSONBlob(resp):
		default:
		}
	}
}

/*
handleSnapshot returns a JSON snapshot of the full timeline.
*/
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

var promptHandler func(prompt string) (string, map[string]float64)

/*
SetPromptHandler installs the callback used when the viz UI sends a prompt.
*/
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
