package visualizer

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Server serves the 3D visualization and streams value events via WebSocket.
*/
type Server struct {
	state   *errnie.State
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	upgrade websocket.Upgrader
	httpSrv *http.Server
	udpConn *net.UDPConn
	pool    *pool.Pool
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

		// Forward raw JSON directly to websockets!
		var event telemetry.Event

		errnie.GuardVoid(server.state, func() error {
			return json.Unmarshal(buf[:n], &event)
		})

		server.Broadcast(event)
	}
}

/*
ListenAndServe starts the HTTP server on the given address.
*/
func (server *Server) ListenAndServe(addr string) error {
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
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
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
