package telemetry

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/errnie"
)

// NewUDPSender returns a broadcast function that JSON-encodes events and
// sends them to the given UDP address (e.g. "127.0.0.1:8258"). The
// returned function is safe for concurrent use.
type UDPSender struct {
	conn         net.Conn
	writeDropped atomic.Uint64
}

func NewUDPSender(addr string) (*UDPSender, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return &UDPSender{conn: conn}, nil
}

func (s *UDPSender) Send(ev Event) {
	if s == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		errnie.Debug(
			"telemetry.UDPSender.Send",
			"op", "json.Marshal",
			"err", err,
			"event_type", fmt.Sprintf("%T", ev),
		)
		return
	}
	_, werr := s.conn.Write(data)
	if werr != nil {
		s.writeDropped.Add(1)
		errnie.Warn(
			"telemetry.UDPSender.Send",
			"op", "write",
			"err", werr,
			"dropped_total", s.writeDropped.Load(),
		)
	}
}

// WriteDropped returns how many UDP payload writes failed after Connect succeeded.
func (s *UDPSender) WriteDropped() uint64 {
	if s == nil {
		return 0
	}
	return s.writeDropped.Load()
}

func (s *UDPSender) Close() error {
	if s == nil {
		return nil
	}
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}
