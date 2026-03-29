package visualizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/primitive"
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
	mu         sync.RWMutex
	clients    map[*websocket.Conn]bool
	upgrade    websocket.Upgrader
	httpSrv    *http.Server
	udpConn    *net.UDPConn
	promptFunc PromptFunc
	ingestFunc IngestFunc
}

/*
NewServer instantiates a visualization server.
*/
func NewServer() *Server {
	return &Server{
		clients: make(map[*websocket.Conn]bool),
		upgrade: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

/*
listenUDP starts the locked UDP listener, forwarding all telemetry to visualizer clients.
*/
func (server *Server) listenUDP(conn *net.UDPConn) error {
	buf := make([]byte, 65535)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}

		if n == primitive.ByteSize {
			server.BroadcastValueFrame(buf[:n])
			continue
		}

		event := telemetry.DecodeBinary(buf[:n])
		if event.Timestamp == 0 {
			event.Timestamp = time.Now().UnixNano()
		}
		server.Broadcast(event)
	}
}

/*
ListenAndServe starts the HTTP server on the given address.
*/
func (server *Server) ListenAndServe(addr string, udpAddr string) error {
	// These 2 lines are only required if you're using mutex or block profiling
	// Read the explanation below for how to set these rates:
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	udpListenAddr, err := resolveUDPListenAddr(addr, udpAddr)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", udpListenAddr)
	if err != nil {
		return err
	}

	server.udpConn = conn

	go func() {
		if err := server.listenUDP(conn); err != nil && !errors.Is(err, net.ErrClosed) {
			// UDP listener stopped.
		}
	}()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/layout", server.handleLayout)
	mux.HandleFunc("/api/system", server.handleSystem)
	mux.HandleFunc("/ws", server.handleWS)

	server.httpSrv = &http.Server{Addr: addr, Handler: mux}

	return server.httpSrv.ListenAndServe()
}

/*
handleLayout returns the current Value layout so the browser can decode live
binary frames without guessing the field offsets.
*/
func (server *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(BuildValueLayout()); err != nil {
		log.Printf("visualizer: handleLayout encode: %v", err)
		http.Error(w, "failed to encode layout", http.StatusInternalServerError)
	}
}

/*
handleSystem returns the runtime topology around the chamber.
*/
func (server *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(BuildSystemTopology()); err != nil {
		log.Printf("visualizer: handleSystem encode: %v", err)
		http.Error(w, "failed to encode system topology", http.StatusInternalServerError)
	}
}

/*
resolveUDPListenAddr derives the UDP bind address from the HTTP listen address
unless an explicit UDP address is provided.
*/
func resolveUDPListenAddr(httpAddr string, udpAddr string) (*net.UDPAddr, error) {
	if udpAddr != "" {
		return net.ResolveUDPAddr("udp", udpAddr)
	}

	host, portText, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return nil, err
	}

	if host == "" {
		host = "127.0.0.1"
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}

	return net.ResolveUDPAddr(
		"udp",
		net.JoinHostPort(host, strconv.Itoa(port+1)),
	)
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
	conn, err := server.upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}

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
			Timestamp: time.Now().UnixNano(),
			Data: telemetry.EventData{
				Stage:   "prompt-error",
				Message: "no machine connected",
			},
		})

		return
	}

	server.Broadcast(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:   "prompt-start",
			Message: msg,
		},
	})

	result, err := fn(msg)

	if err != nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Timestamp: time.Now().UnixNano(),
			Data: telemetry.EventData{
				Stage:   "prompt-error",
				Message: err.Error(),
			},
		})

		return
	}

	resultText := string(result)
	if len(result) == primitive.ByteSize {
		if value := primitive.BytesToValue(result); value != nil {
			resultText = primitive.DecodeTokensToText(value)
		}
	}

	stage := "prompt-complete"
	if len(result) == 0 {
		stage = "prompt-empty"
	}

	server.Broadcast(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:       stage,
			Message:     fmt.Sprintf("%d bytes", len(result)),
			ResultText:  resultText,
			ChunkText:   telemetry.ASCIIFramePreview(result, 120),
			Instruction: "",
		},
	})
}

func (server *Server) handleIngestCommand(text string) {
	server.mu.RLock()
	fn := server.ingestFunc
	server.mu.RUnlock()

	if fn == nil {
		server.Broadcast(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Timestamp: time.Now().UnixNano(),
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
			Timestamp: time.Now().UnixNano(),
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
	msg, err := json.Marshal(event)
	if err != nil {
		return
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	for conn := range server.clients {
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}

/*
BroadcastValueFrame pushes a raw 1024-byte Value frame to clients as a binary
WebSocket message so the browser can map the buffer without JSON overhead.
*/
func (server *Server) BroadcastValueFrame(frame []byte) {
	if len(frame) != primitive.ByteSize {
		return
	}

	cp := make([]byte, primitive.ByteSize)
	copy(cp, frame)

	server.mu.RLock()
	var dead []*websocket.Conn
	for conn := range server.clients {
		if err := conn.WriteMessage(websocket.BinaryMessage, cp); err != nil {
			log.Printf("visualizer: BroadcastValueFrame: write failed: %v", err)
			dead = append(dead, conn)
		}
	}
	server.mu.RUnlock()

	if len(dead) == 0 {
		return
	}

	server.mu.Lock()
	for _, conn := range dead {
		if server.clients[conn] {
			delete(server.clients, conn)
			_ = conn.Close()
		}
	}
	server.mu.Unlock()
}
