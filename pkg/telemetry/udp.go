package telemetry

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/errnie"
)

// defaultUDPWriteTimeout bounds blocking on a full kernel UDP send buffer.
const defaultUDPWriteTimeout = 500 * time.Millisecond

// NewUDPSender returns a broadcast function that JSON-encodes events and
// sends them to the given UDP address (e.g. "127.0.0.1:8258"). The
// returned function is safe for concurrent use.
type UDPSender struct {
	conn         net.Conn
	writeDropped atomic.Uint64
	writeTimeout time.Duration
}

func NewUDPSender(addr string) (*UDPSender, error) {
	return NewUDPSenderWithWriteTimeout(addr, defaultUDPWriteTimeout)
}

// NewUDPSenderWithWriteTimeout is like NewUDPSender but sets the per-write
// deadline; zero or negative disables write deadlines.
func NewUDPSenderWithWriteTimeout(addr string, writeTimeout time.Duration) (*UDPSender, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return &UDPSender{conn: conn, writeTimeout: writeTimeout}, nil
}

func (s *UDPSender) writeWithDeadline(fn func() error) error {
	if s.conn == nil {
		return nil
	}
	if s.writeTimeout <= 0 {
		return fn()
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return err
	}
	err := fn()
	_ = s.conn.SetWriteDeadline(time.Time{})
	return err
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
	werr := s.writeWithDeadline(func() error {
		_, e := s.conn.Write(data)
		return e
	})
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

func (s *UDPSender) SendFrame(frame []byte) {
	if s == nil || s.conn == nil {
		return
	}
	werr := s.writeWithDeadline(func() error {
		_, e := s.conn.Write(frame)
		return e
	})
	if werr != nil {
		s.writeDropped.Add(1)
		errnie.Warn(
			"telemetry.UDPSender.SendFrame",
			"op", "write",
			"err", werr,
			"dropped_total", s.writeDropped.Load(),
		)
	}
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
