package visualizer

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/data"
)

/*
Server serves the 3D visualization and streams chord events via WebSocket.
*/
type Server struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	upgrade websocket.Upgrader
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
Event is a telemetry event sent to all connected visualization clients.
*/
type Event struct {
	Component string    `json:"component"`
	Action    string    `json:"action"`
	Data      EventData `json:"data"`
}

/*
EventData carries the payload for a visualization event.
*/
type EventData struct {
	// Chord identity
	ChordID int    `json:"chordId,omitempty"`
	Bin     int    `json:"bin,omitempty"`
	State   string `json:"state,omitempty"`

	// Chord state
	ActiveBits []int   `json:"activeBits,omitempty"`
	Density    float64 `json:"density,omitempty"`
	ChunkText  string  `json:"chunkText,omitempty"`

	// Evaluation
	Residue    int   `json:"residue,omitempty"`
	MatchBits  []int `json:"matchBits,omitempty"`
	CancelBits []int `json:"cancelBits,omitempty"`

	// LSM edges
	Left  int `json:"left,omitempty"`
	Right int `json:"right,omitempty"`
	Pos   int `json:"pos,omitempty"`

	// Counts
	Paths  int `json:"paths,omitempty"`
	Chunks int `json:"chunks,omitempty"`
	Edges  int `json:"edges,omitempty"`
}

/*
ListenAndServe starts the HTTP server on the given address.
*/
func (server *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("visualizer/static")))
	mux.HandleFunc("/ws", server.handleWS)

	return http.ListenAndServe(addr, mux)
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
func (server *Server) Broadcast(event Event) {
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
EmitChord broadcasts a chord creation event with its active bit positions.
*/
func (server *Server) EmitChord(chord data.Chord, chunkText string, chordID int) {
	bits := data.ChordPrimeIndices(&chord)
	bin := data.ChordBin(&chord)

	server.Broadcast(Event{
		Component: "Tokenizer",
		Action:    "Chord",
		Data: EventData{
			ChordID:    chordID,
			Bin:        bin,
			State:      "stored",
			ActiveBits: bits,
			Density:    chord.ShannonDensity(),
			ChunkText:  chunkText,
		},
	})
}

/*
EmitEvaluate broadcasts an evaluation event showing the XOR residue.
*/
func (server *Server) EmitEvaluate(prompt, match, residue data.Chord, energy int) {
	promptBits := data.ChordPrimeIndices(&prompt)
	matchBits := data.ChordPrimeIndices(&match)

	// Cancel bits = bits that were in prompt AND match (cancelled by XOR)
	intersection := prompt.AND(match)
	cancelBits := data.ChordPrimeIndices(&intersection)

	// Residue bits = bits that survived XOR
	residueBits := data.ChordPrimeIndices(&residue)

	server.Broadcast(Event{
		Component: "Cortex",
		Action:    "Evaluate",
		Data: EventData{
			ActiveBits: promptBits,
			MatchBits:  matchBits,
			CancelBits: cancelBits,
			Residue:    energy,
			Density:    residue.ShannonDensity(),
		},
	})

	_ = residueBits
}

/*
EmitBoundary broadcasts a Sequencer boundary detection event.
*/
func (server *Server) EmitBoundary(chunkCount int, density float64) {
	server.Broadcast(Event{
		Component: "Sequencer",
		Action:    "Boundary",
		Data: EventData{
			Chunks:  chunkCount,
			Density: density,
		},
	})
}

/*
EmitLSMEdge broadcasts a byte-to-byte transition for LSM visualization.
*/
func (server *Server) EmitLSMEdge(left, right byte, pos int, edgeCount int) {
	server.Broadcast(Event{
		Component: "LSM",
		Action:    "Insert",
		Data: EventData{
			Left:  int(left),
			Right: int(right),
			Pos:   pos,
			Edges: edgeCount,
		},
	})
}
