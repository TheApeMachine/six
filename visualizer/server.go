package visualizer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
PromptFunc is the callback signature for handling prompts from the UI.
*/
type PromptFunc func(msg string) ([]byte, error)

/*
IngestFunc is the callback signature for ingesting training data from the UI.
*/
type IngestFunc func(raw []byte) error

/*
wsCommand is a JSON message received from a WebSocket client.
*/
type wsCommand struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

/*
Server serves the 3D visualization and streams value events via WebSocket.
*/
type Server struct {
	state      *errnie.State
	mu         sync.RWMutex
	clients    map[*websocket.Conn]bool
	upgrade    websocket.Upgrader
	httpSrv    *http.Server
	udpConn    *net.UDPConn
	pool       *pool.Pool
	promptFunc PromptFunc
	ingestFunc IngestFunc
}

/*
NewServer instantiates a visualization server.
*/
func NewServer() *Server {
	return &Server{
		state:   errnie.NewState("visualizer/server"),
		clients: make(map[*websocket.Conn]bool),
		upgrade: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		pool: pool.New(
			context.Background(),
			1,
			runtime.NumCPU(),
			&pool.Config{},
		),
	}
}

/*
listenUDP starts the locked UDP listener, forwarding all telemetry to visualizer clients.
*/
func (server *Server) listenUDP() error {
	addr := errnie.Guard(server.state, func() (*net.UDPAddr, error) {
		return net.ResolveUDPAddr("udp", "127.0.0.1:8258")
	})

	conn := errnie.Guard(server.state, func() (*net.UDPConn, error) {
		return net.ListenUDP("udp", addr)
	})

	server.udpConn = conn
	defer conn.Close()

	buf := make([]byte, 65535)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}

		event := telemetry.DecodeBinary(buf[:n])
		server.Broadcast(event)
	}
}

/*
ListenAndServe starts the HTTP server on the given address.
*/
func (server *Server) ListenAndServe(addr string) error {
	// These 2 lines are only required if you're using mutex or block profiling
	// Read the explanation below for how to set these rates:
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	server.pool.Schedule(
		"visualizer/server/listen-udp",
		func(ctx context.Context) (any, error) {
			errnie.GuardVoid(server.state, server.listenUDP)
			return nil, server.state.Err()
		},
		pool.WithTTL(time.Second),
	)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("visualizer/static")))
	mux.HandleFunc("/ws", server.handleWS)

	server.httpSrv = &http.Server{Addr: addr, Handler: mux}

	return server.httpSrv.ListenAndServe()
}

/*
Shutdown gracefully stops the HTTP server and UDP listener.
*/
func (server *Server) Shutdown() {
	if server.udpConn != nil {
		server.udpConn.Close()
	}

	if server.httpSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		server.httpSrv.Shutdown(shutdownCtx)
	}

	if server.pool != nil {
		server.pool.Close()
	}
}

/*
SetPromptFunc registers the callback invoked when a client sends a prompt.
*/
func (server *Server) SetPromptFunc(fn PromptFunc) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.promptFunc = fn
}

/*
SetIngestFunc registers the callback invoked when a client sends training data.
*/
func (server *Server) SetIngestFunc(fn IngestFunc) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.ingestFunc = fn
}

func (server *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn := errnie.Guard(server.state, func() (*websocket.Conn, error) {
		return server.upgrade.Upgrade(w, r, nil)
	})

	server.mu.Lock()
	server.clients[conn] = true
	server.mu.Unlock()

	defer func() {
		server.mu.Lock()
		delete(server.clients, conn)
		server.mu.Unlock()
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var cmd wsCommand

		if json.Unmarshal(msg, &cmd) != nil {
			continue
		}

		switch cmd.Type {
		case "prompt":
			if cmd.Message != "" {
				server.handlePromptCommand(cmd.Message)
			}
		case "ingest":
			if cmd.Message != "" {
				server.handleIngestCommand(cmd.Message)
			}
		}
	}
}

func (server *Server) handlePromptCommand(msg string) {
	server.mu.RLock()
	fn := server.promptFunc
	server.mu.RUnlock()

	if fn == nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage:   "prompt-error",
				Message: "no machine connected",
			},
		})

		return
	}

	result, err := fn(msg)

	if err != nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage:   "prompt-error",
				Message: err.Error(),
			},
		})

		return
	}

	_ = result
}

func (server *Server) handleIngestCommand(text string) {
	server.mu.RLock()
	fn := server.ingestFunc
	server.mu.RUnlock()

	if fn == nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage:   "ingest-error",
				Message: "no machine connected",
			},
		})

		return
	}

	server.Broadcast(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:   "ingest-start",
			Message: fmt.Sprintf("%d bytes", len(text)),
		},
	})

	if err := fn([]byte(text)); err != nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage:   "ingest-error",
				Message: err.Error(),
			},
		})

		return
	}

	server.Broadcast(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:   "ingest-complete",
			Message: fmt.Sprintf("%d bytes ingested", len(text)),
		},
	})
}

/*
Broadcast sends an event to all connected WebSocket clients.
*/
func (server *Server) Broadcast(event telemetry.Event) {
	msg := errnie.Guard(server.state, func() ([]byte, error) {
		return json.Marshal(event)
	})

	server.mu.RLock()
	defer server.mu.RUnlock()

	for conn := range server.clients {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}
