package network

import (
	"context"
	"net"
)

/*
UDPMulticast provides LAN-scoped broadcast transport via UDP multicast.
One datagram = one Value (1024 bytes fits inside a single Ethernet MTU).
Write broadcasts to the multicast group. Read receives from any member.
Follows the same pattern as the Google Cloud multicast reference client.
*/
type UDPMulticast struct {
	err    error
	ctx    context.Context
	cancel context.CancelFunc
	conn   *net.UDPConn
	group  *net.UDPAddr
	dialed bool
}

/*
udpMulticastOption configures a UDPMulticast transport at construction time.
*/
type udpMulticastOption func(*UDPMulticast)

/*
NewUDPMulticast constructs a UDP multicast transport. Use
UDPMulticastWithListener on receivers and UDPMulticastWithDialer on senders.
*/
func NewUDPMulticast(opts ...udpMulticastOption) *UDPMulticast {
	ctx, cancel := context.WithCancel(context.Background())

	conn := &UDPMulticast{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(conn)
	}

	return conn
}

/*
Read receives one UDP datagram from the multicast group into p.
The caller should provide at least 1024 bytes to capture a full Value.
*/
func (udp *UDPMulticast) Read(p []byte) (int, error) {
	if udp.conn == nil {
		return 0, &TransportError{Layer: "udp", Op: "read", Err: ErrUDPNotBound}
	}

	return udp.conn.Read(p)
}

/*
Write broadcasts p to the multicast group as a single datagram.
A dialed socket uses Write (destination is already set). A listener
socket uses WriteToUDP with the explicit group address.
*/
func (udp *UDPMulticast) Write(p []byte) (int, error) {
	if udp.conn == nil {
		return 0, &TransportError{Layer: "udp", Op: "write", Err: ErrUDPNotBound}
	}

	if udp.dialed {
		return udp.conn.Write(p)
	}

	return udp.conn.WriteToUDP(p, udp.group)
}

/*
Close leaves the multicast group and releases the socket.
*/
func (udp *UDPMulticast) Close() error {
	udp.cancel()

	if udp.conn == nil {
		return nil
	}

	return udp.conn.Close()
}

// Ready reports whether the multicast socket is bound/open.
func (udp *UDPMulticast) Ready(ctx context.Context) error {
	_ = ctx
	if udp.conn == nil {
		return &TransportError{Layer: "udp", Op: "ready", Err: ErrUDPNotBound}
	}
	return nil
}

/*
UDPMulticastWithListener joins the multicast group and listens for
inbound datagrams. iface selects the network interface; empty string
picks the system default.
*/
func UDPMulticastWithListener(group string, iface string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		addr, err := net.ResolveUDPAddr("udp4", group)

		if err != nil {
			udp.err = err
			return
		}

		var ifi *net.Interface

		if iface != "" {
			ifi, err = net.InterfaceByName(iface)

			if err != nil {
				udp.err = err
				return
			}
		}

		conn, err := net.ListenMulticastUDP("udp4", ifi, addr)

		if err != nil {
			udp.err = err
			return
		}

		conn.SetReadBuffer(1500)

		udp.conn = conn
		udp.group = addr
	}
}

/*
UDPMulticastWithDialer opens a UDP socket connected to the multicast group
for sending. Write calls go directly to the group without needing an
explicit address on every call.
*/
func UDPMulticastWithDialer(group string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		addr, err := net.ResolveUDPAddr("udp4", group)

		if err != nil {
			udp.err = err
			return
		}

		conn, err := net.DialUDP("udp4", nil, addr)

		if err != nil {
			udp.err = err
			return
		}

		udp.conn = conn
		udp.group = addr
		udp.dialed = true
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
UDPMulticastError is a typed error for UDP multicast transport failures.
*/
type UDPMulticastError string

const (
	ErrUDPNotBound UDPMulticastError = "udp: no socket bound"
)

/*
Error implements the error interface for UDPMulticastError.
*/
func (udpErr UDPMulticastError) Error() string {
	return string(udpErr)
}
