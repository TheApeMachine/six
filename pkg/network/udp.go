package network

import (
	"context"
	"errors"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

/*
UDPMulticast provides LAN-scoped broadcast transport over native UDP multicast.
One write maps to one datagram. Listener mode joins the multicast group and
receives from any member. Dialer mode sends datagrams to the group.
*/
type UDPMulticast struct {
	err              error
	ctx              context.Context
	cancel           context.CancelFunc
	sub              *net.UDPConn
	pub              *net.UDPConn
	group            *net.UDPAddr
	timeout          time.Duration
	readPollDeadline time.Duration
	dialed           bool
	monitor          *transportMonitor
}

type udpMulticastOption func(*UDPMulticast)

/*
NewUDPMulticast constructs a native UDP multicast transport.
Use UDPMulticastWithListener on receivers and UDPMulticastWithDialer on senders.
*/
func NewUDPMulticast(opts ...udpMulticastOption) *UDPMulticast {
	ctx, cancel := context.WithCancel(context.Background())

	udp := &UDPMulticast{
		ctx:     ctx,
		cancel:  cancel,
		timeout: 10 * time.Second,
		monitor: newTransportMonitor(TransportTraits{
			Name:            "udp-multicast",
			Topology:        TransportTopologyLAN,
			Reliable:        false,
			Ordered:         false,
			MessageOriented: true,
			Broadcast:       true,
			Encrypted:       false,
			ExternalRuntime: false,
		}),
	}

	for _, opt := range opts {
		opt(udp)
	}

	if udp.readPollDeadline <= 0 {
		udp.readPollDeadline = 50 * time.Millisecond
	}

	return udp
}

/*
Read receives the next UDP datagram from the joined multicast group.
*/
func (udp *UDPMulticast) Read(p []byte) (int, error) {
	if err := udp.monitor.Allow("udp", "read"); err != nil {
		return 0, err
	}

	if udp.sub == nil {
		err := &NetworkError{
			Subsystem: "udp",
			Op:        "read",
			Mode:      TransportFailureNotReady,
			Systemic:  true,
			Err:       ErrUDPNotBound,
		}
		udp.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return 0, err
	}

	poll := udp.readPollDeadline
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}

	for {
		if err := udp.sub.SetReadDeadline(time.Now().Add(poll)); err != nil {
			udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
			return 0, err
		}

		n, _, err := udp.sub.ReadFromUDP(p)
		if err == nil {
			udp.monitor.RecordSuccess()
			return n, nil
		}

		if udp.ctx.Err() != nil {
			mode := TransportFailureCanceled
			if udp.ctx.Err() == context.DeadlineExceeded {
				mode = TransportFailureTimeout
			}
			udp.monitor.RecordFailure(mode, udp.ctx.Err(), false)
			return 0, udp.ctx.Err()
		}

		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			continue
		}

		mode, systemic := udp.classifyNetError(err)
		udp.monitor.RecordFailure(mode, err, systemic)
		return 0, err
	}
}

/*
Write publishes one UDP datagram to the multicast group.
*/
func (udp *UDPMulticast) Write(p []byte) (int, error) {
	if err := udp.monitor.Allow("udp", "write"); err != nil {
		return 0, err
	}

	if udp.pub == nil {
		err := &NetworkError{
			Subsystem: "udp",
			Op:        "write",
			Mode:      TransportFailureNotReady,
			Systemic:  true,
			Err:       ErrUDPNotBound,
		}
		udp.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return 0, err
	}

	if err := udp.pub.SetWriteDeadline(time.Now().Add(udp.timeout)); err != nil {
		udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
		return 0, err
	}

	return finishMonitoredRW(udp.monitor, udp.classifyNetError, func() (int, error) {
		return udp.pub.Write(p)
	})
}

/*
Close releases the multicast sockets.
*/
func (udp *UDPMulticast) Close() error {
	udp.cancel()

	var firstErr error

	if udp.sub != nil {
		if err := udp.sub.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if udp.pub != nil {
		if err := udp.pub.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

/*
Ready reports whether the multicast transport has the required sockets bound.
*/
func (udp *UDPMulticast) Ready(ctx context.Context) error {
	_ = ctx

	if udp.pub == nil && udp.sub == nil {
		err := &NetworkError{
			Subsystem: "udp",
			Op:        "ready",
			Mode:      TransportFailureNotReady,
			Systemic:  true,
			Err:       ErrUDPNotBound,
		}
		udp.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return err
	}

	udp.monitor.RecordReady()
	return nil
}

/*
Traits reports the transport semantics of UDP multicast.
*/
func (udp *UDPMulticast) Traits() TransportTraits {
	return udp.monitor.Traits()
}

/*
Status reports the current health snapshot of UDP multicast.
*/
func (udp *UDPMulticast) Status() TransportStatus {
	return udp.monitor.Status()
}

/*
UDPMulticastWithListener joins the multicast group and binds a receiving socket.
The same transport also creates a sender socket so listener-side writes work.
*/
func UDPMulticastWithListener(group string, iface string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		groupAddress, err := net.ResolveUDPAddr("udp4", group)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureBind, err, true)
			return
		}

		networkInterface, err := multicastInterface(iface)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureBind, err, true)
			return
		}

		listener, err := net.ListenMulticastUDP("udp4", networkInterface, groupAddress)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureBind, err, true)
			return
		}

		if err := listener.SetReadBuffer(1 << 20); err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
			_ = listener.Close()
			return
		}

		publisher, err := net.DialUDP("udp4", nil, groupAddress)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureDial, err, true)
			_ = listener.Close()
			return
		}

		if networkInterface != nil {
			if err := ipv4.NewPacketConn(publisher).SetMulticastInterface(networkInterface); err != nil {
				udp.err = err
				udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
				_ = listener.Close()
				_ = publisher.Close()
				return
			}
		}

		if err := ipv4.NewPacketConn(publisher).SetMulticastLoopback(true); err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
			_ = listener.Close()
			_ = publisher.Close()
			return
		}

		udp.group = groupAddress
		udp.sub = listener
		udp.pub = publisher
		udp.dialed = false
	}
}

/*
UDPMulticastWithDialer opens a sender socket connected to the multicast group.
*/
func UDPMulticastWithDialer(group string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		groupAddress, err := net.ResolveUDPAddr("udp4", group)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureDial, err, true)
			return
		}

		publisher, err := net.DialUDP("udp4", nil, groupAddress)
		if err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureDial, err, false)
			return
		}

		if err := ipv4.NewPacketConn(publisher).SetMulticastLoopback(true); err != nil {
			udp.err = err
			udp.monitor.RecordFailure(TransportFailureProtocol, err, true)
			_ = publisher.Close()
			return
		}

		udp.group = groupAddress
		udp.pub = publisher
		udp.dialed = true
	}
}

/*
UDPMulticastWithReadPollDeadline sets how long each ReadFromUDP spin waits
before refreshing context cancellation. Values <= 0 are replaced with the
default (50ms) when the transport is constructed.
*/
func UDPMulticastWithReadPollDeadline(deadline time.Duration) udpMulticastOption {
	return func(udp *UDPMulticast) {
		udp.readPollDeadline = deadline
	}
}

/*
UDPMulticastWithContext replaces the default background context.
*/
func UDPMulticastWithContext(ctx context.Context) udpMulticastOption {
	return func(udp *UDPMulticast) {
		udp.cancel()
		udp.ctx, udp.cancel = context.WithCancel(ctx)
	}
}

/*
UDPMulticastWithAeronDir is retained for API stability and is a no-op.
*/
func UDPMulticastWithAeronDir(dir string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		_ = dir
	}
}

func multicastInterface(name string) (*net.Interface, error) {
	if name == "" {
		return nil, nil
	}

	return net.InterfaceByName(name)
}

func (udp *UDPMulticast) classifyNetError(err error) (TransportFailureMode, bool) {
	if err == nil {
		return TransportFailureNone, false
	}

	if err == context.Canceled {
		return TransportFailureCanceled, false
	}

	if err == context.DeadlineExceeded {
		return TransportFailureTimeout, false
	}

	if errors.Is(err, net.ErrClosed) {
		return TransportFailureClosed, true
	}

	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return TransportFailureTimeout, false
	}

	return TransportFailureProtocol, false
}

// UDPMulticastError is a typed error for UDP multicast transport failures.
type UDPMulticastError string

const (
	ErrUDPNotBound UDPMulticastError = "udp: no socket bound"
)

// Error implements the error interface for UDPMulticastError.
func (udpErr UDPMulticastError) Error() string { return string(udpErr) }
