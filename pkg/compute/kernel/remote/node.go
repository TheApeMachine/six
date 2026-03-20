package remote

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/store/dmt"
)

/*
Node wraps a Cap'n Proto RadixRPC client to a single remote dmt peer as an
io.ReadWriteCloser. Write sends raw key bytes to the remote Forest via
RadixRPC.Insert. Read drains any buffered response or sync data. Available
reports 1 when the connection is live, 0 otherwise.

The connection is established lazily on first Write or explicitly via connect.
*/
type Node struct {
	ctx     context.Context
	cancel  context.CancelFunc
	addr    string
	conn    net.Conn
	rpcConn *rpc.Conn
	client  dmt.RadixRPC
	outBuf  bytes.Buffer
	mu      sync.Mutex
	alive   bool
}

type nodeOpts func(*Node)

/*
NewNode creates a remote Node targeting the given address. The underlying
TCP + RadixRPC connection is not opened until connect is called (or the
first Write triggers it).
*/
func NewNode(opts ...nodeOpts) *Node {
	node := &Node{}

	for _, opt := range opts {
		opt(node)
	}

	return node
}

/*
Write sends p as the key of a RadixRPC.Insert to the remote peer. Each
call is one Insert. The connection is established lazily on the first
Write if it has not been opened yet.
*/
func (node *Node) Write(p []byte) (n int, err error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if !node.alive {
		if connErr := node.connectLocked(); connErr != nil {
			return 0, connErr
		}
	}

	future, release := node.client.Insert(
		node.ctx,
		func(params dmt.RadixRPC_insert_Params) error {
			if setErr := params.SetKey(p); setErr != nil {
				return setErr
			}

			if setErr := params.SetValue(nil); setErr != nil {
				return setErr
			}

			params.SetTerm(0)
			params.SetLogIndex(0)

			return nil
		},
	)

	result, rpcErr := future.Struct()

	if rpcErr != nil {
		release()
		node.alive = false
		return 0, rpcErr
	}

	ok := result.IsValid() && result.Success()
	release()

	if !ok {
		return 0, NewNodeError(NodeErrorRemoteRejected)
	}

	return len(p), nil
}

/*
Read drains any buffered response data from the remote peer. Returns
io.EOF when the buffer is empty.
*/
func (node *Node) Read(p []byte) (n int, err error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	return node.outBuf.Read(p)
}

/*
Close tears down the RadixRPC connection and the underlying TCP socket.
Benign "use of closed network connection" errors from the peer tearing down
first are swallowed; real errors propagate.
*/
func (node *Node) Close() error {
	node.mu.Lock()
	defer node.mu.Unlock()

	node.alive = false

	var closeErr error

	if node.rpcConn != nil {
		if err := node.rpcConn.Close(); err != nil && !isClosedConnErr(err) {
			closeErr = err
		}

		node.rpcConn = nil
	}

	if node.conn != nil {
		if err := node.conn.Close(); err != nil && !isClosedConnErr(err) {
			closeErr = err
		}

		node.conn = nil
	}

	if node.cancel != nil {
		node.cancel()
	}

	return closeErr
}

/*
isClosedConnErr returns true for "use of closed network connection" which
is benign during simultaneous TCP teardown from both ends.
*/
func isClosedConnErr(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

/*
Available reports 1 when the TCP + RPC connection is live, 0 otherwise.
*/
func (node *Node) Available() (int, error) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if node.alive {
		return 1, nil
	}

	return 0, nil
}

/*
Sync sends a RadixRPC.Sync to the remote peer and buffers any diff entries
returned. The buffered data is available via subsequent Read calls. Each
diff entry is written as [key_len:4][key][value_len:4][value].
*/
func (node *Node) Sync(merkleRoot []byte, term, logIndex uint64) error {
	node.mu.Lock()
	defer node.mu.Unlock()

	if !node.alive {
		return NewNodeError(NodeErrorNotConnected)
	}

	future, release := node.client.Sync(
		node.ctx,
		func(params dmt.RadixRPC_sync_Params) error {
			if setErr := params.SetMerkleRoot(merkleRoot); setErr != nil {
				return setErr
			}

			params.SetTerm(term)
			params.SetLogIndex(logIndex)

			return nil
		},
	)

	result, rpcErr := future.Struct()

	if rpcErr != nil {
		release()
		return rpcErr
	}

	diff, diffErr := result.Diff()

	if diffErr != nil {
		release()
		return diffErr
	}

	entries, entriesErr := diff.Entries()

	if entriesErr != nil {
		release()
		return entriesErr
	}

	for idx := 0; idx < entries.Len(); idx++ {
		entry := entries.At(idx)
		key, _ := entry.Key()
		value, _ := entry.Value()

		node.outBuf.Write(key)
		node.outBuf.Write(value)
	}

	release()

	return nil
}

/*
Addr returns the remote peer address this Node targets.
*/
func (node *Node) Addr() string {
	return node.addr
}

/*
connectLocked dials the remote address and bootstraps the RadixRPC client.
Caller must hold node.mu.
*/
func (node *Node) connectLocked() error {
	conn, dialErr := net.DialTimeout("tcp", node.addr, 5*time.Second)

	if dialErr != nil {
		return dialErr
	}

	node.conn = conn
	transport := rpc.NewStreamTransport(conn)
	node.rpcConn = rpc.NewConn(transport, nil)
	node.client = dmt.RadixRPC(node.rpcConn.Bootstrap(node.ctx))
	node.alive = true

	return nil
}

/*
NodeWithContext sets a cancellable context on the Node.
*/
func NodeWithContext(ctx context.Context) nodeOpts {
	return func(node *Node) {
		node.ctx, node.cancel = context.WithCancel(ctx)
	}
}

/*
NodeWithAddress sets the target peer address (host:port).
*/
func NodeWithAddress(addr string) nodeOpts {
	return func(node *Node) {
		node.addr = addr
	}
}

/*
NodeError is a typed error for Node failures.
*/
type NodeError string

const (
	NodeErrorNotConnected   NodeError = "node: not connected"
	NodeErrorRemoteRejected NodeError = "node: remote rejected insert"
)

/*
NewNodeError creates a NodeError.
*/
func NewNodeError(err NodeError) NodeError {
	return err
}

/*
Error implements the error interface.
*/
func (nodeErr NodeError) Error() string {
	return string(nodeErr)
}
