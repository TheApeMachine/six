package network

import (
	"context"
	"time"

	"github.com/lirm/aeron-go/aeron"
	"github.com/lirm/aeron-go/aeron/atomic"
	"github.com/lirm/aeron-go/aeron/idlestrategy"
	"github.com/lirm/aeron-go/aeron/logbuffer"
)

const aeronIPCChannel = "aeron:ipc"

/*
IPC provides same-machine transport over Aeron IPC (shared memory).
A running Aeron media driver is required before use. The listener side
derives stream IDs from the socket path and waits in Accept; the dialer
side mirrors those IDs and is ready immediately.

The Read/Write/Close interface is identical to the previous Unix-socket
implementation so callers require no changes.
*/
type IPC struct {
	err       error
	ctx       context.Context
	cancel    context.CancelFunc
	client    *aeron.Aeron
	pub       *aeron.Publication
	sub       *aeron.Subscription
	recvCh    chan []byte
	aeronDir  string
	timeout   time.Duration
	pubStream int32
	subStream int32
	owner     bool // true for listen-side
}

type ipcOption func(*IPC)

/*
NewIPC constructs an Aeron IPC transport. Pass IPCWithListen on the
server side and IPCWithDial on the client side, both using the same
path string to derive matching stream IDs.
*/
func NewIPC(opts ...ipcOption) *IPC {
	ctx, cancel := context.WithCancel(context.Background())

	i := &IPC{
		ctx:     ctx,
		cancel:  cancel,
		recvCh:  make(chan []byte, 256),
		timeout: 10 * time.Second,
	}

	for _, opt := range opts {
		opt(i)
	}

	// If neither stream is configured (bare NewIPC()) skip driver connection.
	if i.pubStream == 0 && i.subStream == 0 {
		return i
	}

	aeronCtx := aeron.NewContext().
		MediaDriverTimeout(i.timeout).
		ErrorHandler(func(err error) { i.err = err })

	if i.aeronDir != "" {
		aeronCtx = aeronCtx.AeronDir(i.aeronDir)
	}

	client, err := aeron.Connect(aeronCtx)
	if err != nil {
		i.err = err
		return i
	}

	i.client = client

	if i.pubStream != 0 {
		pub, err := client.AddPublication(aeronIPCChannel, i.pubStream)
		if err != nil {
			i.err = err
			return i
		}

		i.pub = pub
	}

	if i.subStream != 0 {
		sub, err := client.AddSubscription(aeronIPCChannel, i.subStream)
		if err != nil {
			i.err = err
			return i
		}

		i.sub = sub
		i.startPoller()
	}

	return i
}

/*
Read receives the next message from the Aeron subscription. Blocks until
a message arrives or the context is cancelled.
*/
func (ipc *IPC) Read(p []byte) (int, error) {
	if ipc.sub == nil {
		return 0, ErrIPCNotConnected
	}

	select {
	case <-ipc.ctx.Done():
		return 0, ipc.ctx.Err()
	case msg := <-ipc.recvCh:
		n := copy(p, msg)
		return n, nil
	}
}

/*
Write offers p as a single Aeron message. Retries on back-pressure until
the publication accepts it or the context is cancelled.
*/
func (ipc *IPC) Write(p []byte) (int, error) {
	if ipc.pub == nil {
		return 0, ErrIPCNotConnected
	}

	buf := atomic.MakeBuffer(p)
	idle := idlestrategy.Sleeping{SleepFor: time.Millisecond}

	for {
		ret := ipc.pub.Offer(buf, 0, int32(len(p)), nil)

		if ret >= 0 {
			return len(p), nil
		}

		if ret == aeron.PublicationClosed {
			return 0, ErrIPCNotConnected
		}

		select {
		case <-ipc.ctx.Done():
			return 0, ipc.ctx.Err()
		default:
			idle.Idle(0)
		}
	}
}

/*
Close shuts down the publication, subscription, and media driver client.
*/
func (ipc *IPC) Close() error {
	ipc.cancel()

	if ipc.pub != nil {
		ipc.pub.Close()
	}

	if ipc.sub != nil {
		ipc.sub.Close()
	}

	if ipc.client != nil {
		ipc.client.Close()
	}

	return nil
}

/*
Accept blocks until the remote side connects (publication becomes
connected). Must be called after constructing with IPCWithListen.
*/
func (ipc *IPC) Accept() error {
	if !ipc.owner {
		return ErrIPCNotListening
	}

	if ipc.pub == nil {
		return ErrIPCNotConnected
	}

	for !ipc.pub.IsConnected() {
		select {
		case <-ipc.ctx.Done():
			return ipc.ctx.Err()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// Ready blocks until the publication is connected (a subscriber is
// present). No-op when no publication is configured.
func (ipc *IPC) Ready(ctx context.Context) error {
	if ipc.err != nil {
		return ipc.err
	}

	if ipc.pub == nil {
		return nil
	}

	for !ipc.pub.IsConnected() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// startPoller runs a background goroutine that polls the subscription
// and forwards each received message to recvCh.
func (ipc *IPC) startPoller() {
	handler := func(buf *atomic.Buffer, offset, length int32, _ *logbuffer.Header) {
		data := buf.GetBytesArray(offset, length)
		cp := make([]byte, len(data))
		copy(cp, data)

		select {
		case ipc.recvCh <- cp:
		default: // drop when full — caller is not consuming fast enough
		}
	}

	idle := idlestrategy.Sleeping{SleepFor: time.Millisecond}

	go func() {
		for {
			select {
			case <-ipc.ctx.Done():
				return
			default:
				n := ipc.sub.Poll(handler, 10)
				idle.Idle(n)
			}
		}
	}()
}

// ipcStreamIDs derives a matched pair of stream IDs from a path string
// so that listener and dialer automatically use complementary streams.
func ipcStreamIDs(path string) (serverPub, serverSub int32) {
	var h uint32 = 2166136261 // FNV-1a offset basis

	for i := 0; i < len(path); i++ {
		h ^= uint32(path[i])
		h *= 16777619
	}

	base := int32(h%900) + 100
	return base, base + 1000
}

/*
IPCWithListen configures the listener side. Stream IDs are derived from
path so that a matching IPCWithDial(path) peer routes correctly. The
Aeron media driver must be reachable at the default or configured dir.
*/
func IPCWithListen(path string) ipcOption {
	return func(i *IPC) {
		serverPub, serverSub := ipcStreamIDs(path)
		i.pubStream = serverPub
		i.subStream = serverSub
		i.owner = true
	}
}

/*
IPCWithDial connects to a listener created with IPCWithListen(path).
Stream IDs are derived from path to mirror the listener's configuration.
*/
func IPCWithDial(path string) ipcOption {
	return func(i *IPC) {
		serverPub, serverSub := ipcStreamIDs(path)
		// Client TX → server RX, client RX ← server TX
		i.pubStream = serverSub
		i.subStream = serverPub
		i.owner = false
	}
}

/*
IPCWithAeronDir overrides the Aeron media driver directory. Defaults to
the driver's own configured directory (usually /dev/shm/aeron-<user>).
*/
func IPCWithAeronDir(dir string) ipcOption {
	return func(i *IPC) { i.aeronDir = dir }
}

/*
IPCWithTimeout sets how long to wait for the media driver on connect.
*/
func IPCWithTimeout(d time.Duration) ipcOption {
	return func(i *IPC) { i.timeout = d }
}

// IPCError is a typed error for IPC transport failures.
type IPCError string

const (
	ErrIPCNotConnected IPCError = "ipc: no active connection"
	ErrIPCNotListening IPCError = "ipc: no listener configured"
)

// Error implements the error interface for IPCError.
func (ipcErr IPCError) Error() string { return string(ipcErr) }
