package network

import (
	"net"
	"os"
)

/*
IPC provides same-machine transport over Unix domain sockets via the
stdlib net package. A listener side creates the socket; a dialer side
connects to it. The underlying net.Conn already implements
io.ReadWriteCloser, so Read/Write/Close delegate directly.

For future zero-copy upgrades (mmap-backed shared memory), the
io.ReadWriteCloser interface makes the swap transparent to callers.
*/
type IPC struct {
	err    error
	conn   net.Conn
	listen net.Listener
	path   string
	owner  bool
}

/*
ipcOption configures an IPC transport at construction time.
*/
type ipcOption func(*IPC)

/*
NewIPC constructs an IPC transport. Use IPCWithListen on the server
side and IPCWithDial on the client side, both pointing to the same
Unix socket path.
*/
func NewIPC(opts ...ipcOption) *IPC {
	conn := &IPC{}

	for _, opt := range opts {
		opt(conn)
	}

	return conn
}

/*
Read receives bytes from the connected peer.
*/
func (ipc *IPC) Read(p []byte) (int, error) {
	if ipc.conn == nil {
		return 0, ErrIPCNotConnected
	}

	return ipc.conn.Read(p)
}

/*
Write sends bytes to the connected peer.
*/
func (ipc *IPC) Write(p []byte) (int, error) {
	if ipc.conn == nil {
		return 0, ErrIPCNotConnected
	}

	return ipc.conn.Write(p)
}

/*
Close shuts down the connection and, if this side owns the listener,
closes it and removes the socket file.
*/
func (ipc *IPC) Close() error {
	var firstErr error

	if ipc.conn != nil {
		if err := ipc.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if ipc.listen != nil {
		if err := ipc.listen.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if ipc.owner && ipc.path != "" {
		os.Remove(ipc.path)
	}

	return firstErr
}

/*
Accept blocks until a client connects to the listening socket. Must
be called after constructing with IPCWithListen.
*/
func (ipc *IPC) Accept() error {
	if ipc.listen == nil {
		return ErrIPCNotListening
	}

	conn, err := ipc.listen.Accept()

	if err != nil {
		return err
	}

	ipc.conn = conn

	return nil
}

/*
IPCWithListen creates a Unix domain socket listener at the given path.
Any stale socket file at the path is removed first. Call Accept
separately to wait for an inbound connection.
*/
func IPCWithListen(path string) ipcOption {
	return func(ipc *IPC) {
		os.Remove(path)

		listen, err := net.Listen("unix", path)

		if err != nil {
			ipc.err = err
			return
		}

		ipc.listen = listen
		ipc.path = path
		ipc.owner = true
	}
}

/*
IPCWithDial connects to an existing Unix domain socket at the given
path. The transport is ready for Read/Write immediately.
*/
func IPCWithDial(path string) ipcOption {
	return func(ipc *IPC) {
		conn, err := net.Dial("unix", path)

		if err != nil {
			ipc.err = err
			return
		}

		ipc.conn = conn
		ipc.path = path
		ipc.owner = false
	}
}

/*
IPCError is a typed error for IPC transport failures.
*/
type IPCError string

const (
	ErrIPCNotConnected IPCError = "ipc: no active connection"
	ErrIPCNotListening IPCError = "ipc: no listener configured"
)

/*
Error implements the error interface for IPCError.
*/
func (ipcErr IPCError) Error() string {
	return string(ipcErr)
}
