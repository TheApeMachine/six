package network

import (
	"context"
	"fmt"
	"time"

	"github.com/lirm/aeron-go/aeron"
	"github.com/lirm/aeron-go/aeron/atomic"
	"github.com/lirm/aeron-go/aeron/idlestrategy"
	"github.com/lirm/aeron-go/aeron/logbuffer"
)

const defaultUDPStreamID = int32(2001)

/*
UDPMulticast provides LAN-scoped broadcast transport via Aeron UDP
multicast. One message = one Aeron offer. Write publishes to the
multicast group; Read receives from any member.

A running Aeron media driver is required. The channel URI is built from
the multicast group address and optional interface, following Aeron's
aeron:udp?endpoint=<group>|interface=<iface> convention.
*/
type UDPMulticast struct {
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	client   *aeron.Aeron
	pub      *aeron.Publication
	sub      *aeron.Subscription
	recvCh   chan []byte
	aeronDir string
	timeout  time.Duration
	dialed   bool // true when constructed as a sender (dial side)
}

type udpMulticastOption func(*UDPMulticast)

/*
NewUDPMulticast constructs an Aeron UDP multicast transport.
Use UDPMulticastWithListener on receivers and UDPMulticastWithDialer on senders.
*/
func NewUDPMulticast(opts ...udpMulticastOption) *UDPMulticast {
	ctx, cancel := context.WithCancel(context.Background())

	u := &UDPMulticast{
		ctx:     ctx,
		cancel:  cancel,
		recvCh:  make(chan []byte, 256),
		timeout: 10 * time.Second,
	}

	for _, opt := range opts {
		opt(u)
	}

	return u
}

/*
Read receives the next message from the Aeron subscription.
Blocks until a message arrives or the context is cancelled.
*/
func (udp *UDPMulticast) Read(p []byte) (int, error) {
	if udp.sub == nil {
		return 0, &TransportError{Layer: "udp", Op: "read", Err: ErrUDPNotBound}
	}

	select {
	case <-udp.ctx.Done():
		return 0, udp.ctx.Err()
	case msg := <-udp.recvCh:
		n := copy(p, msg)
		return n, nil
	}
}

/*
Write publishes p to the Aeron UDP multicast channel. Retries on
back-pressure until the publication accepts it or the context is cancelled.
*/
func (udp *UDPMulticast) Write(p []byte) (int, error) {
	if udp.pub == nil {
		return 0, &TransportError{Layer: "udp", Op: "write", Err: ErrUDPNotBound}
	}

	buf := atomic.MakeBuffer(p)
	idle := idlestrategy.Sleeping{SleepFor: time.Millisecond}

	for {
		ret := udp.pub.Offer(buf, 0, int32(len(p)), nil)

		if ret >= 0 {
			return len(p), nil
		}

		if ret == aeron.PublicationClosed {
			return 0, &TransportError{Layer: "udp", Op: "write", Err: ErrUDPNotBound}
		}

		select {
		case <-udp.ctx.Done():
			return 0, udp.ctx.Err()
		default:
			idle.Idle(0)
		}
	}
}

/*
Close tears down the publication, subscription, and media driver client.
*/
func (udp *UDPMulticast) Close() error {
	udp.cancel()

	if udp.pub != nil {
		udp.pub.Close()
	}

	if udp.sub != nil {
		udp.sub.Close()
	}

	if udp.client != nil {
		udp.client.Close()
	}

	return nil
}

// Ready reports whether the UDP transport is bound.
func (udp *UDPMulticast) Ready(ctx context.Context) error {
	_ = ctx
	if udp.pub == nil && udp.sub == nil {
		return &TransportError{Layer: "udp", Op: "ready", Err: ErrUDPNotBound}
	}

	return nil
}

// connectAeron creates an Aeron client and stores it on udp.
// Returns false and sets udp.err on failure.
func (udp *UDPMulticast) connectAeron() bool {
	aeronCtx := aeron.NewContext().
		MediaDriverTimeout(udp.timeout).
		ErrorHandler(func(err error) { udp.err = err })

	if udp.aeronDir != "" {
		aeronCtx = aeronCtx.AeronDir(udp.aeronDir)
	}

	client, err := aeron.Connect(aeronCtx)
	if err != nil {
		udp.err = err
		return false
	}

	udp.client = client
	return true
}

// startPoller runs a background goroutine that polls the subscription.
func (udp *UDPMulticast) startPoller() {
	handler := func(buf *atomic.Buffer, offset, length int32, _ *logbuffer.Header) {
		data := buf.GetBytesArray(offset, length)
		cp := make([]byte, len(data))
		copy(cp, data)

		select {
		case udp.recvCh <- cp:
		default:
		}
	}

	idle := idlestrategy.Sleeping{SleepFor: time.Millisecond}

	go func() {
		for {
			select {
			case <-udp.ctx.Done():
				return
			default:
				n := udp.sub.Poll(handler, 10)
				idle.Idle(n)
			}
		}
	}()
}

/*
UDPMulticastWithListener joins the multicast group and sets up an Aeron
subscription. iface selects the network interface; empty string lets the
OS pick. The listener also creates a publication for the WriteToUDP path.
*/
func UDPMulticastWithListener(group string, iface string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		if !udp.connectAeron() {
			return
		}

		subChannel := fmt.Sprintf("aeron:udp?endpoint=%s", group)
		if iface != "" {
			subChannel += fmt.Sprintf("|interface=%s", iface)
		}

		sub, err := udp.client.AddSubscription(subChannel, defaultUDPStreamID)
		if err != nil {
			udp.err = err
			return
		}

		udp.sub = sub
		udp.startPoller()

		// Listener-side publication uses the same channel for write capability.
		pub, err := udp.client.AddPublication(subChannel, defaultUDPStreamID)
		if err != nil {
			udp.err = err
			return
		}

		udp.pub = pub
		udp.dialed = false
	}
}

/*
UDPMulticastWithDialer opens an Aeron publication connected to the
multicast group for sending.
*/
func UDPMulticastWithDialer(group string) udpMulticastOption {
	return func(udp *UDPMulticast) {
		if !udp.connectAeron() {
			return
		}

		channel := fmt.Sprintf("aeron:udp?endpoint=%s", group)

		pub, err := udp.client.AddPublication(channel, defaultUDPStreamID)
		if err != nil {
			udp.err = err
			return
		}

		udp.pub = pub
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
UDPMulticastWithAeronDir overrides the Aeron media driver directory.
*/
func UDPMulticastWithAeronDir(dir string) udpMulticastOption {
	return func(udp *UDPMulticast) { udp.aeronDir = dir }
}

// UDPMulticastError is a typed error for UDP multicast transport failures.
type UDPMulticastError string

const (
	ErrUDPNotBound UDPMulticastError = "udp: no socket bound"
)

// Error implements the error interface for UDPMulticastError.
func (udpErr UDPMulticastError) Error() string { return string(udpErr) }
