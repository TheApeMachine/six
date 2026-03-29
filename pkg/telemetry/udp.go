package telemetry

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/errnie"
)

// defaultUDPWriteTimeout bounds blocking on a full kernel UDP send buffer.
const defaultUDPWriteTimeout = 500 * time.Millisecond

const (
	defaultDropBurstThreshold = 100
	defaultDropBurstWindow    = 50 * time.Millisecond
	defaultBackoffBase        = 50 * time.Millisecond
	defaultBackoffMax         = 2 * time.Second
)

// NewUDPSender returns a broadcast function that JSON-encodes events and
// sends them to the given UDP address (e.g. "127.0.0.1:8258"). The
// returned function is safe for concurrent use.
type UDPSender struct {
	conn         net.Conn
	writeDropped atomic.Uint64
	writeShed    atomic.Uint64
	writeTimeout time.Duration

	stateMu            sync.Mutex
	dropBurstStart     time.Time
	dropBurstCount     uint64
	backoffUntil       time.Time
	currentBackoff     time.Duration
	dropBurstThreshold uint64
	dropBurstWindow    time.Duration
	backoffBase        time.Duration
	backoffMax         time.Duration
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
	return &UDPSender{
		conn:               conn,
		writeTimeout:       writeTimeout,
		dropBurstThreshold: defaultDropBurstThreshold,
		dropBurstWindow:    defaultDropBurstWindow,
		backoffBase:        defaultBackoffBase,
		backoffMax:         defaultBackoffMax,
	}, nil
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

func (s *UDPSender) shouldShed(now time.Time) bool {
	if s == nil {
		return false
	}

	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	return now.Before(s.backoffUntil)
}

func (s *UDPSender) recordWriteDrop(op string, err error) {
	if s == nil {
		return
	}

	droppedTotal := s.writeDropped.Add(1)
	now := time.Now()

	var (
		backoffApplied bool
		backoffFor     time.Duration
		windowDrops    uint64
	)

	s.stateMu.Lock()
	if s.dropBurstStart.IsZero() || now.Sub(s.dropBurstStart) > s.dropBurstWindow {
		s.dropBurstStart = now
		s.dropBurstCount = 0
	}
	s.dropBurstCount++
	windowDrops = s.dropBurstCount

	if s.dropBurstCount >= s.dropBurstThreshold {
		if s.currentBackoff <= 0 {
			s.currentBackoff = s.backoffBase
		} else {
			s.currentBackoff *= 2
			if s.currentBackoff > s.backoffMax {
				s.currentBackoff = s.backoffMax
			}
		}
		s.backoffUntil = now.Add(s.currentBackoff)
		s.dropBurstStart = now
		s.dropBurstCount = 0
		backoffFor = s.currentBackoff
		backoffApplied = true
	}
	s.stateMu.Unlock()

	errnie.Warn(
		"telemetry.UDPSender.write_drop",
		"op", op,
		"err", err,
		"dropped_total", droppedTotal,
		"dropped_window", windowDrops,
	)

	if backoffApplied {
		errnie.Warn(
			"telemetry.UDPSender.backpressure",
			"op", op,
			"backoff", backoffFor.String(),
			"dropped_total", droppedTotal,
		)
	}
}

func (s *UDPSender) recordWriteSuccess() {
	if s == nil {
		return
	}

	now := time.Now()

	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if now.Before(s.backoffUntil) {
		return
	}
	if s.currentBackoff > 0 {
		s.currentBackoff /= 2
		if s.currentBackoff < s.backoffBase {
			s.currentBackoff = 0
		}
	}
}

func (s *UDPSender) Send(ev Event) {
	if s == nil {
		return
	}
	if s.shouldShed(time.Now()) {
		s.writeShed.Add(1)
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
		s.recordWriteDrop("send", werr)
		return
	}
	s.recordWriteSuccess()
}

// WriteDropped returns how many UDP payload writes failed after Connect succeeded.
func (s *UDPSender) WriteDropped() uint64 {
	if s == nil {
		return 0
	}
	return s.writeDropped.Load()
}

// WriteShed returns how many events were intentionally skipped during adaptive backoff.
func (s *UDPSender) WriteShed() uint64 {
	if s == nil {
		return 0
	}
	return s.writeShed.Load()
}

func (s *UDPSender) SendFrame(frame []byte) {
	if s == nil || s.conn == nil {
		return
	}
	if s.shouldShed(time.Now()) {
		s.writeShed.Add(1)
		return
	}
	werr := s.writeWithDeadline(func() error {
		_, e := s.conn.Write(frame)
		return e
	})
	if werr != nil {
		s.recordWriteDrop("send_frame", werr)
		return
	}
	s.recordWriteSuccess()
}

// Emit implements the Emitter interface. It stamps the event timestamp if zero
// and delegates to Send.
func (s *UDPSender) Emit(ev Event) {
	if ev.Timestamp == 0 {
		ev.Timestamp = time.Now().UnixNano()
	}
	s.Send(ev)
}

// EmitFrame implements the Emitter interface by delegating to SendFrame.
func (s *UDPSender) EmitFrame(frame []byte) {
	s.SendFrame(frame)
}

var _ Emitter = (*UDPSender)(nil)

func (s *UDPSender) Close() error {
	if s == nil {
		return nil
	}
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}
