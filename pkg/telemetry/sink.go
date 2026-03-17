package telemetry

import (
	"encoding/json"
	"net"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Sink is a fire-and-forget UDP telemetry emitter for the 3D visualizer.
It broadcasts structural state events non-blockingly, guaranteeing no
performance degradation on the core processes.
*/
type Sink struct {
	address string
	conn    *net.UDPConn
	state   *errnie.State
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
	sink := &Sink{
		address: "127.0.0.1:8258",
		state:   errnie.NewState("telemetry/sink"),
	}

	for _, opt := range opts {
		opt(sink)
	}

	addr := errnie.Guard(sink.state, func() (*net.UDPAddr, error) {
		return net.ResolveUDPAddr("udp", sink.address)
	})

	sink.conn = errnie.Guard(sink.state, func() (*net.UDPConn, error) {
		return net.DialUDP("udp", nil, addr)
	})

	return sink
}

/*
WithAddress configures the UDP endpoint for the sink.
*/
func WithAddress(address string) opts {
	return func(s *Sink) {
		s.address = address
	}
}

/*
Emit sends the telemetry event as JSON via UDP.
When the visualization server is offline, write failures are dropped silently.
*/
func (sink *Sink) Emit(event Event) {
	if sink.conn == nil {
		return
	}

	raw := errnie.Guard(sink.state, func() ([]byte, error) {
		return json.Marshal(event)
	})

	_, _ = sink.conn.Write(raw)
}
