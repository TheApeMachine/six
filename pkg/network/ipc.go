package network

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"
)

/*
IPC provides same-machine transport over Unix domain sockets.
The listener side binds a Unix socket path and accepts exactly one peer.
The dialer side connects to that path eagerly during construction.

The Read/Write/Close interface stays stable so callers do not care
whether the transport is backed by shared memory or sockets.
*/
type IPC struct {
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	listener *net.UnixListener
	conn     net.Conn
	timeout  time.Duration
	path     string
	owner    bool
	monitor  *transportMonitor
}

type ipcOption func(*IPC)

/*
NewIPC constructs an IPC transport over Unix domain sockets.
Pass IPCWithListen on the server side and IPCWithDial on the client side.
*/
func NewIPC(opts ...ipcOption) *IPC {
	ctx, cancel := context.WithCancel(context.Background())

	ipc := &IPC{
		ctx:     ctx,
		cancel:  cancel,
		timeout: 10 * time.Second,
		monitor: newTransportMonitor(TransportTraits{
			Name:            "ipc",
			Topology:        TransportTopologySameMachine,
			Reliable:        true,
			Ordered:         true,
			MessageOriented: true,
			Broadcast:       false,
			Encrypted:       false,
			ExternalRuntime: false,
		}),
	}

	for _, opt := range opts {
		opt(ipc)
	}

	if ipc.path == "" {
		return ipc
	}

	if ipc.owner {
		if err := ipc.listen(); err != nil {
			ipc.err = err
			ipc.monitor.RecordFailure(TransportFailureBind, err, true)
		}

		return ipc
	}

	if err := ipc.dial(); err != nil {
		ipc.err = err
		ipc.monitor.RecordFailure(TransportFailureDial, err, false)
	}

	return ipc
}

/*
Read receives bytes from the active Unix socket connection.
*/
func (ipc *IPC) Read(p []byte) (int, error) {
	if err := ipc.monitor.Allow("ipc", "read"); err != nil {
		return 0, err
	}

	connection, err := ipc.ensureConn(false)
	if err != nil {
		ipc.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return 0, err
	}

	n, err := connection.Read(p)
	if err != nil {
		mode, systemic := ipc.classifyNetError(err)
		ipc.monitor.RecordFailure(mode, err, systemic)
		return n, err
	}

	ipc.monitor.RecordSuccess()
	return n, nil
}

/*
Write sends bytes over the active Unix socket connection.
*/
func (ipc *IPC) Write(p []byte) (int, error) {
	if err := ipc.monitor.Allow("ipc", "write"); err != nil {
		return 0, err
	}

	connection, err := ipc.ensureConn(false)
	if err != nil {
		ipc.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return 0, err
	}

	n, err := connection.Write(p)
	if err != nil {
		mode, systemic := ipc.classifyNetError(err)
		ipc.monitor.RecordFailure(mode, err, systemic)
		return n, err
	}

	ipc.monitor.RecordSuccess()
	return n, nil
}

/*
Close shuts down the accepted connection and listener socket.
*/
func (ipc *IPC) Close() error {
	ipc.cancel()

	var firstErr error

	if ipc.conn != nil {
		if err := ipc.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if ipc.listener != nil {
		if err := ipc.listener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if ipc.owner && ipc.path != "" {
		if err := os.Remove(ipc.path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

/*
Accept blocks until a client connects to the listening socket.
*/
func (ipc *IPC) Accept() error {
	if err := ipc.monitor.Allow("ipc", "accept"); err != nil {
		return err
	}

	if !ipc.owner {
		err := &NetworkError{
			Subsystem:    "ipc",
			Op:       "accept",
			Mode:     TransportFailureBind,
			Systemic: true,
			Err:      ErrIPCNotListening,
		}
		ipc.monitor.RecordFailure(TransportFailureBind, err, true)
		return err
	}

	if ipc.listener == nil {
		err := &NetworkError{
			Subsystem:    "ipc",
			Op:       "accept",
			Mode:     TransportFailureNotReady,
			Systemic: true,
			Err:      ErrIPCNotConnected,
		}
		ipc.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return err
	}

	if ipc.conn != nil {
		ipc.monitor.RecordReady()
		return nil
	}

	deadline := time.Now().Add(ipc.timeout)
	if err := ipc.listener.SetDeadline(deadline); err != nil {
		ipc.monitor.RecordFailure(TransportFailureProtocol, err, true)
		return err
	}

	connection, err := ipc.listener.Accept()
	if err != nil {
		mode, systemic := ipc.classifyNetError(err)
		ipc.monitor.RecordFailure(mode, err, systemic)
		return err
	}

	ipc.conn = connection
	ipc.monitor.RecordReady()
	return nil
}

/*
Ready waits until the IPC transport has an active connection.
Dialers are ready immediately after a successful constructor dial.
Listeners normalize readiness through Accept.
*/
func (ipc *IPC) Ready(ctx context.Context) error {
	if ipc.err != nil {
		ipc.monitor.RecordFailure(TransportFailureDependency, ipc.err, true)
		return ipc.err
	}

	if ipc.conn != nil {
		ipc.monitor.RecordReady()
		return nil
	}

	if !ipc.owner {
		if ipc.path == "" {
			err := &NetworkError{
				Subsystem:    "ipc",
				Op:       "ready",
				Mode:     TransportFailureNotReady,
				Systemic: true,
				Err:      ErrIPCDialUnconfigured,
			}
			ipc.monitor.RecordFailure(TransportFailureNotReady, err, true)
			return err
		}

		err := &NetworkError{
			Subsystem:    "ipc",
			Op:       "ready",
			Mode:     TransportFailureNotReady,
			Systemic: true,
			Err:      ErrIPCNotConnected,
		}
		ipc.monitor.RecordFailure(TransportFailureNotReady, err, true)
		return err
	}

	if ctx == nil {
		ctx = ipc.ctx
	}

	if ctx == nil {
		ctx = context.Background()
	}

	ready := make(chan error, 1)
	go func() {
		ready <- ipc.Accept()
	}()

	select {
	case <-ctx.Done():
		mode := TransportFailureCanceled
		if ctx.Err() == context.DeadlineExceeded {
			mode = TransportFailureTimeout
		}
		ipc.monitor.RecordFailure(mode, ctx.Err(), false)
		_ = ipc.Close()
		<-ready
		return ctx.Err()
	case err := <-ready:
		return err
	}
}

/*
Traits reports the transport semantics of IPC.
*/
func (ipc *IPC) Traits() TransportTraits {
	return ipc.monitor.Traits()
}

/*
Status reports the current health snapshot of IPC.
*/
func (ipc *IPC) Status() TransportStatus {
	return ipc.monitor.Status()
}

func (ipc *IPC) listen() error {
	if ipc.path == "" {
		return nil
	}

	_ = os.Remove(ipc.path)

	address, err := net.ResolveUnixAddr("unix", ipc.path)
	if err != nil {
		return err
	}

	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return err
	}

	ipc.listener = listener
	return nil
}

func (ipc *IPC) dial() error {
	if ipc.path == "" {
		return nil
	}

	address, err := net.ResolveUnixAddr("unix", ipc.path)
	if err != nil {
		return err
	}

	connection, err := net.DialTimeout("unix", address.String(), ipc.timeout)
	if err != nil {
		return err
	}

	ipc.conn = connection
	ipc.monitor.RecordReady()
	return nil
}

func (ipc *IPC) ensureConn(allowAccept bool) (net.Conn, error) {
	if ipc.conn != nil {
		return ipc.conn, nil
	}

	if ipc.path == "" {
		return nil, ErrIPCNotConnected
	}

	if allowAccept && ipc.owner {
		if err := ipc.Accept(); err != nil {
			return nil, err
		}

		return ipc.conn, nil
	}

	if ipc.owner {
		return nil, ErrIPCNotConnected
	}

	if err := ipc.dial(); err != nil {
		return nil, err
	}

	return ipc.conn, nil
}

func (ipc *IPC) classifyNetError(err error) (TransportFailureMode, bool) {
	if err == nil {
		return TransportFailureNone, false
	}

	if err == context.Canceled {
		return TransportFailureCanceled, false
	}

	if err == context.DeadlineExceeded {
		return TransportFailureTimeout, false
	}

	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return TransportFailureClosed, true
	}

	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return TransportFailureTimeout, false
	}

	return TransportFailureProtocol, false
}

/*
IPCWithListen configures the listener side on the given Unix socket path.
*/
func IPCWithListen(path string) ipcOption {
	return func(ipc *IPC) {
		ipc.path = path
		ipc.owner = true
	}
}

/*
IPCWithDial connects to a listener created with IPCWithListen(path).
*/
func IPCWithDial(path string) ipcOption {
	return func(ipc *IPC) {
		ipc.path = path
		ipc.owner = false
	}
}

/*
IPCWithAeronDir is retained for API stability and is a no-op for Unix sockets.
*/
func IPCWithAeronDir(dir string) ipcOption {
	return func(ipc *IPC) {
		_ = dir
	}
}

/*
IPCWithTimeout sets how long listen-side Accept and dial-side connect may block.
*/
func IPCWithTimeout(duration time.Duration) ipcOption {
	return func(ipc *IPC) {
		if duration <= 0 {
			return
		}

		ipc.timeout = duration
	}
}

// IPCError is a typed error for IPC transport failures.
type IPCError string

const (
	ErrIPCNotConnected IPCError = "ipc: no active connection"
	ErrIPCNotListening IPCError = "ipc: no listener configured"
	// ErrIPCDialUnconfigured is returned when a client-side IPC is constructed
	// without a socket path so Ready cannot reach a peer (Read/Write would hit
	// ensureConn with no destination).
	ErrIPCDialUnconfigured IPCError = "ipc: dial-side IPC has no path configured"
)

// Error implements the error interface for IPCError.
func (ipcErr IPCError) Error() string { return string(ipcErr) }
