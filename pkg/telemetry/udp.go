package telemetry

import (
	"encoding/json"
	"net"
)

// NewUDPSender returns a broadcast function that JSON-encodes events and
// sends them to the given UDP address (e.g. "127.0.0.1:8258"). The
// returned function is safe for concurrent use.
type UDPSender struct {
	conn net.Conn
}

func NewUDPSender(addr string) (*UDPSender, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return &UDPSender{conn: conn}, nil
}

func (s *UDPSender) Send(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.conn.Write(data)
}

func (s *UDPSender) Close() error {
	return s.conn.Close()
}
