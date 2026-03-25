package telemetry

import (
	"encoding/json"
	"net"
)

// NewUDPSender returns a broadcast function that JSON-encodes events and
// sends them to the given UDP address (e.g. "127.0.0.1:8258"). The
// returned function is safe for concurrent use.
func NewUDPSender(addr string) (func(Event), error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return func(ev Event) {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		conn.Write(data)
	}, nil
}
