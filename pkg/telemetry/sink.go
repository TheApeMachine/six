package telemetry

import (
	"net"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Sink is a fire-and-forget UDP telemetry emitter for the 3D visualizer.
Events are encoded as flat binary frames (see wire.go) into a reusable
buffer, so Emit allocates nothing on the hot path.
*/
type Sink struct {
	address string
	conn    *net.UDPConn
	state   *errnie.State
	buf     []byte
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
		buf:     make([]byte, 0, 512),
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
Emit sends the telemetry event as a binary frame via UDP.
The buffer is reused across calls so the hot path is zero-alloc
for events that fit within the pre-allocated capacity.
*/
func (sink *Sink) Emit(event Event) {
	if sink.conn == nil {
		return
	}

	sink.buf = event.AppendBinary(sink.buf[:0])
	_, _ = sink.conn.Write(sink.buf)
}
