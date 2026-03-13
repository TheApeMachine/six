package visualizer

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Server serves the 3D visualization and streams chord events via WebSocket.
*/
type Server struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	upgrade websocket.Upgrader
	httpSrv *http.Server
	udpConn *net.UDPConn
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
func (server *Server) listenUDP() error {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8258")
	if err != nil {
		return console.Error(err, "msg", "failed to resolve visualizer UDP")
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return console.Error(err, "msg", "failed to listen on visualizer UDP")
	}
	server.udpConn = conn
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}

		// Forward raw JSON directly to websockets!
		var event telemetry.Event
		if err := json.Unmarshal(buf[:n], &event); err == nil {
			server.Broadcast(event)
		}
	}
	return nil
}

/*
ListenAndServe starts the HTTP server on the given address.
*/
func (server *Server) ListenAndServe(addr string) error {
	go func() {
		if err := server.listenUDP(); err != nil {
			console.Error(err, "msg", "UDP listen failed loop")
		}
	}()

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
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
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
