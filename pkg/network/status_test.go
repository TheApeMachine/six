package network

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
)

type stubManagedTransport struct {
	buffer  []byte
	readErr error
	monitor *transportMonitor
	closed  bool
}

func newStubManagedTransport(options ...monitorOption) *stubManagedTransport {
	monitor := newTransportMonitor(TransportTraits{
		Name:            "stub",
		Topology:        TransportTopologyWAN,
		Reliable:        true,
		Ordered:         true,
		MessageOriented: true,
		Encrypted:       true,
	}, options...)

	return &stubManagedTransport{
		buffer:  make([]byte, 0, 1024),
		monitor: monitor,
	}
}

func (transport *stubManagedTransport) Read(p []byte) (int, error) {
	if err := transport.monitor.Allow("stub", "read"); err != nil {
		return 0, err
	}

	if transport.readErr != nil {
		transport.monitor.RecordFailure(TransportFailureProtocol, transport.readErr, false)
		return 0, transport.readErr
	}

	n := copy(p, transport.buffer)
	transport.monitor.RecordSuccess()
	return n, nil
}

func (transport *stubManagedTransport) Write(p []byte) (int, error) {
	if err := transport.monitor.Allow("stub", "write"); err != nil {
		return 0, err
	}

	transport.buffer = append(transport.buffer[:0], p...)
	transport.monitor.RecordSuccess()
	return len(p), nil
}

func (transport *stubManagedTransport) Close() error {
	transport.closed = true
	return nil
}

func (transport *stubManagedTransport) Ready(ctx context.Context) error {
	_ = ctx
	transport.monitor.RecordReady()
	return nil
}

func (transport *stubManagedTransport) Traits() TransportTraits {
	return transport.monitor.Traits()
}

func (transport *stubManagedTransport) Status() TransportStatus {
	return transport.monitor.Status()
}

func TestTransportMonitor(t *testing.T) {
	gc.Convey("Given a transport monitor with a forgiving breaker", t, func() {
		monitor := newTransportMonitor(
			TransportTraits{Name: "test", Topology: TransportTopologyWAN},
			MonitorWithBreaker(NewCircuitBreaker(
				BreakerWithFailureThreshold(3),
				BreakerWithCooldown(10*time.Millisecond),
			)),
		)

		gc.Convey("A systemic failure should open the circuit immediately", func() {
			failure := errors.New("driver missing")
			monitor.RecordFailure(TransportFailureDependency, failure, true)

			status := monitor.Status()
			gc.So(status.Breaker, gc.ShouldEqual, CircuitOpen)
			gc.So(status.SystemicFailure, gc.ShouldBeTrue)
			gc.So(status.LastFailureMode, gc.ShouldEqual, TransportFailureDependency)
			gc.So(errors.Is(status.LastFailure, failure), gc.ShouldBeTrue)
		})

		gc.Convey("An open circuit should reject further operations until cooldown", func() {
			monitor.RecordFailure(TransportFailureDependency, errors.New("driver missing"), true)

			err := monitor.Allow("test", "write")
			gc.So(errors.Is(err, ErrTransportCircuitOpen), gc.ShouldBeTrue)
		})
	})
}

func TestTransportTraits(t *testing.T) {
	gc.Convey("Given the concrete transports", t, func() {
		ipc := NewIPC()
		udp := NewUDPMulticast()
		quic := NewQUIC()

		gc.Convey("They should expose transport-specific traits", func() {
			gc.So(ipc.Traits().Topology, gc.ShouldEqual, TransportTopologySameMachine)
			gc.So(ipc.Traits().ExternalRuntime, gc.ShouldBeFalse)
			gc.So(udp.Traits().Topology, gc.ShouldEqual, TransportTopologyLAN)
			gc.So(udp.Traits().Broadcast, gc.ShouldBeTrue)
			gc.So(quic.Traits().Topology, gc.ShouldEqual, TransportTopologyWAN)
			gc.So(quic.Traits().Encrypted, gc.ShouldBeTrue)
		})
	})
}

func TestUniConnWithManagedTransport(t *testing.T) {
	gc.Convey("Given a UniConn wired to a managed transport", t, func() {
		transport := newStubManagedTransport()
		conn := NewUniConn(t.Context(), UniConnWithTransport(QUICType, transport))

		gc.Convey("It should delegate readiness, traits, status, and I/O", func() {
			gc.So(conn.Ready(), gc.ShouldBeNil)
			gc.So(conn.Traits().Name, gc.ShouldEqual, "stub")
			gc.So(conn.Status().Ready, gc.ShouldBeTrue)

			n, err := conn.Write([]byte("hello"))
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 5)

			buf := make([]byte, 5)
			n, err = io.ReadFull(conn, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 5)
			gc.So(string(buf), gc.ShouldEqual, "hello")
		})

		gc.Reset(func() {
			conn.Close()
			gc.So(transport.closed, gc.ShouldBeTrue)
		})
	})
}

func TestUniConnStatusWithoutTransport(t *testing.T) {
	gc.Convey("Given a UniConn without any configured transport", t, func() {
		conn := NewUniConn(t.Context())

		gc.Convey("Status should surface the missing-transport condition", func() {
			status := conn.Status()
			gc.So(status.Degraded, gc.ShouldBeTrue)
			gc.So(status.SystemicFailure, gc.ShouldBeTrue)
			gc.So(status.LastFailureMode, gc.ShouldEqual, TransportFailureNotReady)
			gc.So(errors.Is(status.LastFailure, ErrNoTransport), gc.ShouldBeTrue)
		})
	})
}

func BenchmarkTransportMonitor_ClosedCircuit(b *testing.B) {
	monitor := newTransportMonitor(TransportTraits{Name: "bench"})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := monitor.Allow("bench", "write"); err != nil {
			b.Fatal(err)
		}

		monitor.RecordSuccess()
	}
}

func BenchmarkUniConn_ManagedTransportWrite(b *testing.B) {
	transport := newStubManagedTransport()
	conn := NewUniConn(b.Context(), UniConnWithTransport(QUICType, transport))
	if err := conn.Ready(); err != nil {
		b.Fatal(err)
	}

	payload := make([]byte, 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	for b.Loop() {
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
