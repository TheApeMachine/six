package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/store/dmt"
)

const pipelineDepth = 64

/*
DefaultDialTimeout is the TCP dial budget used by Node when no
NodeWithDialTimeout override is set.
*/
const DefaultDialTimeout = 5 * time.Second

/*
inflight holds a pending RPC call that the drainer goroutine will resolve.
*/
type inflight struct {
	future  dmt.RadixRPC_insert_Results_Future
	release capnp.ReleaseFunc
}

/*
Node wraps a Cap'n Proto RadixRPC client to a single remote dmt peer as an
io.ReadWriteCloser. Write sends raw key bytes to the remote Forest via
RadixRPC.Insert. Read drains any buffered response or sync data. Available
reports 1 when the node has an address and no terminal error (including
before the first lazy dial), 0 when unusable.

Writes are pipelined: each call pushes the RPC future into a bounded channel
and returns immediately. A background drainer goroutine resolves futures and
detects remote failures. The channel depth (pipelineDepth) provides natural
backpressure when the remote peer is slow.
*/
type Node struct {
	ctx         context.Context
	cancel      context.CancelFunc
	addr        string
	dialTimeout time.Duration
	conn        net.Conn
	rpcConn     *rpc.Conn
	client      dmt.RadixRPC
	outBuf      bytes.Buffer
	mu          sync.Mutex
	alive       atomic.Bool
	closed      atomic.Bool
	pipe        chan inflight
	lastErr     atomic.Pointer[error]
}

type nodeOpts func(*Node)

/*
NewNode creates a remote Node targeting the given address. The underlying
TCP + RadixRPC connection is not opened until the first Write triggers it.
*/
func NewNode(opts ...nodeOpts) *Node {
	node := &Node{
		pipe:        make(chan inflight, pipelineDepth),
		dialTimeout: DefaultDialTimeout,
	}

	for _, opt := range opts {
		opt(node)
	}

	return node
}

/*
Write sends p as the key of a RadixRPC.Insert to the remote peer. The call
is pipelined: the RPC is dispatched and the future is pushed to the drainer
channel, so Write returns as soon as the message is in the TCP send buffer.

If the drainer detected a remote failure on a previous call, that error is
returned here and the node is marked dead.
*/
func (node *Node) Write(p []byte) (n int, err error) {
	if node.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	if errp := node.lastErr.Load(); errp != nil {
		node.alive.Store(false)
		return 0, *errp
	}

	if !node.alive.Load() {
		node.mu.Lock()
		connErr := node.connectLocked()
		node.mu.Unlock()

		if connErr != nil {
			return 0, connErr
		}
	}

	if node.closed.Load() {
		return 0, io.ErrClosedPipe
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

			params.SetTerm(1)
			params.SetLogIndex(1)

			return nil
		},
	)

	if errp := node.lastErr.Load(); errp != nil {
		release()
		node.alive.Store(false)
		return 0, *errp
	}

	if sendErr := node.enqueue(inflight{future: future, release: release}); sendErr != nil {
		release()
		return 0, sendErr
	}

	return len(p), nil
}

/*
WriteSync sends p as a RadixRPC.Insert and blocks until the remote confirms
success. Use when write ordering or confirmation is required (tests, sync
barriers).
*/
func (node *Node) WriteSync(p []byte) (n int, err error) {
	if !node.alive.Load() {
		node.mu.Lock()
		connErr := node.connectLocked()
		node.mu.Unlock()

		if connErr != nil {
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

			params.SetTerm(1)
			params.SetLogIndex(1)

			return nil
		},
	)

	result, rpcErr := future.Struct()

	if rpcErr != nil {
		release()
		node.alive.Store(false)
		return 0, rpcErr
	}

	ok := result.IsValid() && result.Success()
	release()

	if !ok {
		return 0, NodeErrorRemoteRejected
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
Close tears down the drainer, RadixRPC connection, and TCP socket.
Benign "use of closed network connection" errors from the peer tearing down
first are swallowed; real errors propagate.
*/
func (node *Node) Close() error {
	if !node.closed.CompareAndSwap(false, true) {
		return nil
	}

	node.alive.Store(false)

	if node.cancel != nil {
		node.cancel()
	}

	close(node.pipe)

	node.mu.Lock()
	defer node.mu.Unlock()

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
Available reports 1 when the node can be routed to (address set, not closed,
no terminal error), including before the first lazy dial. Returns 0 after
Close or when unusable.
*/
func (node *Node) Available() (int, error) {
	if node.closed.Load() {
		return 0, nil
	}

	if node.addr == "" {
		return 0, nil
	}

	if errp := node.lastErr.Load(); errp != nil {
		return 0, nil
	}

	return 1, nil
}

/*
Sync sends a RadixRPC.Sync to the remote peer and buffers any diff entries
returned. The buffered data is available via subsequent Read calls.
*/
func (node *Node) Sync(merkleRoot []byte, term, logIndex uint64) error {
	node.mu.Lock()
	defer node.mu.Unlock()

	if !node.alive.Load() {
		return NodeErrorNotConnected
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
		key, keyErr := entry.Key()

		if keyErr != nil {
			release()
			return keyErr
		}

		value, valueErr := entry.Value()

		if valueErr != nil {
			release()
			return valueErr
		}

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
LastError returns the first terminal error observed by the node drainer.
*/
func (node *Node) LastError() error {
	if errp := node.lastErr.Load(); errp != nil {
		return *errp
	}

	return nil
}

/*
connectLocked dials the remote address, bootstraps the RadixRPC client, and
starts the background drainer goroutine. Caller must hold node.mu.
*/
func (node *Node) connectLocked() error {
	if node.closed.Load() {
		return io.ErrClosedPipe
	}

	if node.alive.Load() {
		return nil
	}

	dialTimeout := node.dialTimeout

	if dialTimeout <= 0 {
		dialTimeout = DefaultDialTimeout
	}

	conn, dialErr := net.DialTimeout("tcp", node.addr, dialTimeout)

	if dialErr != nil {
		return dialErr
	}

	node.conn = conn
	transport := rpc.NewStreamTransport(conn)
	node.rpcConn = rpc.NewConn(transport, nil)
	node.client = dmt.RadixRPC(node.rpcConn.Bootstrap(node.ctx))
	node.alive.Store(true)

	go node.drain()

	return nil
}

/*
drain resolves pipelined RPC futures in order. If any call fails, the error
is stored for the next Write to surface and the node is marked dead. The
goroutine exits when the pipe channel is closed (via Close).
*/
func (node *Node) drain() {
	for call := range node.pipe {
		result, rpcErr := call.future.Struct()

		if rpcErr != nil {
			call.release()
			node.storeErr(rpcErr)
			node.alive.Store(false)
			if node.cancel != nil {
				node.cancel()
			}
			node.releasePending()
			return
		}

		ok := result.IsValid() && result.Success()
		call.release()

		if !ok {
			node.storeErr(NodeErrorRemoteRejected)
			node.alive.Store(false)
			if node.cancel != nil {
				node.cancel()
			}
			node.releasePending()
			return
		}
	}
}

/*
storeErr stores an error for the next Write call to pick up.
*/
func (node *Node) storeErr(err error) {
	if err == nil {
		return
	}

	firstErr := err
	node.lastErr.CompareAndSwap(nil, &firstErr)
}

/*
enqueue pushes one in-flight call onto the drainer pipe without panicking if
Close races the send.
*/
func (node *Node) enqueue(call inflight) (err error) {
	if node.closed.Load() {
		return io.ErrClosedPipe
	}

	defer func() {
		if recover() != nil {
			err = io.ErrClosedPipe
		}
	}()

	select {
	case node.pipe <- call:
		return nil
	case <-node.ctx.Done():
		if node.closed.Load() {
			return io.ErrClosedPipe
		}

		return node.ctx.Err()
	}
}

/*
releasePending drops any queued calls after the first fatal drainer failure.
*/
func (node *Node) releasePending() {
	for {
		select {
		case call, ok := <-node.pipe:
			if !ok {
				return
			}

			call.release()
		default:
			return
		}
	}
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
NodeWithDialTimeout sets the TCP dial timeout for lazy connect.
*/
func NodeWithDialTimeout(d time.Duration) nodeOpts {
	return func(node *Node) {
		node.dialTimeout = d
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
Error implements the error interface.
*/
func (nodeErr NodeError) Error() string {
	return string(nodeErr)
}
