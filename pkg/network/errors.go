package network

import "fmt"

// TransportError wraps a network transport failure with the protocol layer,
// operation, optional address, and underlying cause.
type TransportError struct {
	Layer string // "quic", "udp", "ipc", "uniconn"
	Op    string // "read", "write", "accept", "dial", "close"
	Addr  string // remote or local address (when known)
	Err   error  // underlying cause (sentinel or OS-level)
}

func (e *TransportError) Error() string {
	if e.Addr != "" {
		return fmt.Sprintf("%s: %s [%s]: %v", e.Layer, e.Op, e.Addr, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Layer, e.Op, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }
