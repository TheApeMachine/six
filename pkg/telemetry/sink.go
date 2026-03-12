package telemetry

import (
	"encoding/json"
	"net"
)

/*
Sink is a fire-and-forget UDP telemetry emitter for the 3D visualizer.
It broadcasts structural state events non-blockingly, guaranteeing no
performance degradation on the core processes.
*/
type Sink struct {
	conn *net.UDPConn
}

/*
opts configures Sink with options.
*/
type opts func(*Sink)

/*
NewSink instantiates a new Sink and dials the UDP socket.
If the visualization server is offline, packets are silently dropped.
*/
func NewSink(opts ...opts) *Sink {
	sink := &Sink{}

	for _, opt := range opts {
		opt(sink)
	}

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8258")
	if err == nil {
		sink.conn, _ = net.DialUDP("udp", nil, addr)
	}

	return sink
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
	ChordID int    `json:"chordId,omitempty"`
	Bin     int    `json:"bin,omitempty"`
	State   string `json:"state,omitempty"`

	ActiveBits []int   `json:"activeBits,omitempty"`
	Density    float64 `json:"density,omitempty"`
	ChunkText  string  `json:"chunkText,omitempty"`

	Residue    int   `json:"residue,omitempty"`
	MatchBits  []int `json:"matchBits,omitempty"`
	CancelBits []int `json:"cancelBits,omitempty"`

	Left  int `json:"left,omitempty"`
	Right int `json:"right,omitempty"`
	Pos   int `json:"pos,omitempty"`

	Paths  int `json:"paths,omitempty"`
	Chunks int `json:"chunks,omitempty"`
	Edges  int `json:"edges,omitempty"`

	Level      int     `json:"level,omitempty"`
	Theta      float64 `json:"theta,omitempty"`
	ParentBin  int     `json:"parentBin,omitempty"`
	ChildCount int     `json:"childCount,omitempty"`
}

/*
Emit sends the telemetry event as JSON via UDP.
*/
func (sink *Sink) Emit(event Event) {
	if sink.conn == nil {
		return
	}

	raw, err := json.Marshal(event)
	if err == nil {
		sink.conn.Write(raw)
	}
}
