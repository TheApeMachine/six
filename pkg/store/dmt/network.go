/*
package dmt implements distributed networking functionality for the radix tree.
It provides peer-to-peer communication, leader election, and data synchronization
capabilities for maintaining a consistent distributed state across nodes.
*/
package dmt

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
NetworkConfig holds configuration for distributed tree networking.
It defines the network behavior including listen address, peer connections,
node identification, and synchronization parameters.
*/
type NetworkConfig struct {
	// Address to listen on, e.g. ":6380" for all interfaces port 6380
	ListenAddr string
	// List of peer addresses to connect to
	PeerAddrs []string
	// Unique ID for this node
	NodeID string
	// Time between sync attempts with peers
	SyncInterval time.Duration
	// Directory for persisting data
	PersistDir string
}

/*
NetworkNode represents a distributed tree node.
It manages peer connections, handles RPC communication, maintains a merkle tree
for consistency verification, and participates in leader election.
*/
type NetworkNode struct {
	state      *errnie.State
	config     NetworkConfig
	forest     *Forest
	listener   net.Listener
	peers      map[string]*peer
	peersMutex sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	merkleTree *MerkleTree
	metrics    *Metrics
	election   *Election
}

/*
peer represents a connection to another tree node.
It maintains both the raw network connection and the RPC client
for communicating with the remote node.
*/
type peer struct {
	addr    string
	conn    net.Conn
	rpcConn *rpc.Conn
	client  RadixRPC
}

/*
NewNetworkNode creates a new networked tree node.
It initializes the network infrastructure, starts background processes
for connection management and synchronization, and sets up the merkle tree
for consistency verification.
*/
func NewNetworkNode(config NetworkConfig, forest *Forest) (*NetworkNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	node := &NetworkNode{
		state:      errnie.NewState("dmt/network"),
		config:     config,
		forest:     forest,
		peers:      make(map[string]*peer),
		ctx:        ctx,
		cancel:     cancel,
		merkleTree: NewMerkleTree(),
		metrics:    NewMetrics(),
	}

	// Start listener
	node.listener = errnie.Guard(node.state, func() (net.Listener, error) {
		return net.Listen("tcp", config.ListenAddr)
	})

	clusterSize := len(config.PeerAddrs) + 1
	node.election = NewElection(ElectionConfig{
		ElectionTimeout:   150 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
		QuorumSize:        max(1, (clusterSize/2)+1),
	}, node)
	node.election.stateLock.Lock()
	node.election.term = 1
	node.election.stateLock.Unlock()

	node.scheduleLoop("accept-loop", func(ctx context.Context) (any, error) {
		node.acceptLoop()
		return nil, nil
	})

	node.scheduleLoop("connect-loop", func(ctx context.Context) (any, error) {
		node.connectLoop()
		return nil, nil
	})

	node.scheduleLoop("sync-loop", func(ctx context.Context) (any, error) {
		node.syncLoop()
		return nil, nil
	})

	return node, node.state.Err()
}

/*
acceptLoop accepts incoming peer connections.
It runs in the background and handles new peer connection attempts,
creating appropriate handlers for each connection.
*/
func (n *NetworkNode) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.ctx.Done():
				return
			default:
				errnie.Error(err)
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()
		n.schedule("handle-"+remoteAddr, func(ctx context.Context) (any, error) {
			n.handleConnection(conn)
			return nil, nil
		})
	}
}

/*
handleConnection handles incoming peer connections.
It sets up the RPC infrastructure for the connection and maintains
the connection until it is closed by the peer.
*/
func (n *NetworkNode) handleConnection(conn net.Conn) {
	// Create transport from connection
	transport := rpc.NewStreamTransport(conn)

	// Create RPC connection with this node as the bootstrap interface
	main := RadixRPC_ServerToClient(n)
	rpcConn := rpc.NewConn(transport, &rpc.Options{
		BootstrapClient: capnp.Client(main),
	})
	defer rpcConn.Close()

	// Wait for connection to close
	<-rpcConn.Done()
}

/*
connectLoop maintains connections to peers.
It uses a short 100ms tick until every configured peer has connected,
then falls back to a 10s heartbeat tick.
*/
func (n *NetworkNode) connectLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	connectFunc := func() (allConnected bool) {
		allConnected = true

		for _, addr := range n.config.PeerAddrs {
			n.peersMutex.RLock()
			_, exists := n.peers[addr]
			n.peersMutex.RUnlock()

			if !exists {
				allConnected = false
				peerAddr := addr
				n.schedule("connect-"+peerAddr, func(ctx context.Context) (any, error) {
					n.connectToPeer(peerAddr)
					return nil, nil
				})
			}
		}

		return
	}

	connectFunc()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if connectFunc() {
				// All peers connected — slow down to 10s heartbeat.
				ticker.Reset(10 * time.Second)
			}
		}
	}
}

/*
connectToPeer establishes a connection to a peer.
It handles the connection setup, RPC client creation, and maintains
the connection until it is closed.
*/
func (n *NetworkNode) connectToPeer(addr string) {
	state := errnie.NewState("dmt/network/connect")

	conn := errnie.Guard(state, func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	})

	if state.Failed() {
		return
	}

	transport := rpc.NewStreamTransport(conn)
	rpcConn := rpc.NewConn(transport, nil)
	client := RadixRPC(rpcConn.Bootstrap(n.ctx))

	p := &peer{
		addr:    addr,
		conn:    conn,
		rpcConn: rpcConn,
		client:  client,
	}

	n.peersMutex.Lock()
	n.peers[addr] = p
	n.peersMutex.Unlock()

	// Sync immediately now that the peer is registered.
	n.schedule("sync-on-connect-"+addr, func(ctx context.Context) (any, error) {
		n.syncWithPeers()
		return nil, nil
	})

	<-rpcConn.Done()

	// Clean up peer
	n.peersMutex.Lock()
	delete(n.peers, addr)
	n.peersMutex.Unlock()
}

/*
syncLoop periodically syncs with peers.
It ensures data consistency across the network by regularly
initiating synchronization with connected peers.
*/
func (n *NetworkNode) syncLoop() {
	interval := n.config.SyncInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.syncWithPeers()
		}
	}
}

/*
syncWithPeers initiates sync with all connected peers.
It sends the current merkle root to each peer and processes any
differences that need to be synchronized.
*/
func (n *NetworkNode) syncWithPeers() {
	start := time.Now()

	// Update merkle root and get current log state
	n.updateMerkleRoot()
	currentTerm := n.election.getCurrentTerm()
	lastLogIndex := n.election.getLastLogIndex()

	n.peersMutex.RLock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.peersMutex.RUnlock()

	for _, p := range peers {
		peer := p
		n.schedule("sync-peer-"+peer.addr, func(ctx context.Context) (any, error) {
			state := errnie.NewState("dmt/network/sync-peer")
			future, release := peer.client.Sync(n.ctx, func(p RadixRPC_sync_Params) error {
				var rootHash []byte
				if n.merkleTree.Root != nil {
					rootHash = n.merkleTree.Root.Hash
				}
				if err := p.SetMerkleRoot(rootHash); err != nil {
					return err
				}
				p.SetTerm(currentTerm)
				p.SetLogIndex(lastLogIndex)
				return nil
			})
			defer release()

			result := errnie.Guard(state, future.Struct)
			diff := errnie.Guard(state, result.Diff)
			entries := errnie.Guard(state, diff.Entries)

			if state.Failed() {
				return nil, state.Err()
			}

			totalBytes := 0
			for i := 0; i < entries.Len(); i++ {
				entry := entries.At(i)
				key := errnie.Guard(state, entry.Key)
				value := errnie.Guard(state, entry.Value)
				entryTerm := entry.Term()
				entryIndex := entry.Index()

				if state.Failed() {
					continue
				}

				key = append([]byte(nil), key...)
				value = append([]byte(nil), value...)
				totalBytes += len(key) + len(value)
				n.forest.Insert(key, value)
				n.merkleTree.Insert(key, value)
				n.election.updateLogState(entryIndex, entryTerm)
			}

			if entries.Len() > 0 {
				n.merkleTree.Rebuild()
			}

			n.metrics.RecordSync(time.Since(start), totalBytes)
			return nil, state.Err()
		})
	}
}

/*
Insert implements RadixRPC_Server.Insert.
It handles insertion of new data into the local tree and merkle tree,
ensuring consistency across the distributed system.
*/
func (n *NetworkNode) Insert(ctx context.Context, call RadixRPC_insert) error {
	state := errnie.NewState("dmt/network/insert")
	args := call.Args()
	key := errnie.Guard(state, args.Key)
	value := errnie.Guard(state, args.Value)
	key = append([]byte(nil), key...)
	value = append([]byte(nil), value...)

	term := args.Term()
	index := args.LogIndex()

	if term != 0 && term < n.election.getCurrentTerm() {
		return fmt.Errorf("stale term")
	}

	if term == 0 {
		return fmt.Errorf("dmt/network insert: missing term in request")
	}

	// Update local state with log tracking
	n.forest.Insert(key, value)
	n.merkleTree.Insert(key, value)
	n.merkleTree.Rebuild()
	n.election.updateLogState(index, term)
	n.updateMerkleRoot()

	result := errnie.Guard(state, call.AllocResults)

	result.SetSuccess(true)
	result.SetTerm(term)
	result.SetLogIndex(index)
	return state.Err()
}

/*
Sync implements RadixRPC_Server.Sync.
It handles synchronization requests from peers, comparing merkle roots
and sending any necessary updates to maintain consistency.
*/
func (n *NetworkNode) Sync(ctx context.Context, call RadixRPC_sync) error {
	state := errnie.NewState("dmt/network/sync-handler")

	args := call.Args()
	peerRoot := errnie.Guard(state, args.MerkleRoot)
	peerTerm := args.Term()

	if peerTerm > n.election.getCurrentTerm() {
		n.election.stepDown(peerTerm)
		return fmt.Errorf("outdated term")
	}

	result := errnie.Guard(state, call.AllocResults)
	diff := errnie.Guard(state, result.NewDiff)

	if state.Failed() {
		return state.Err()
	}

	var ourRoot []byte
	if n.merkleTree.Root != nil {
		ourRoot = n.merkleTree.Root.Hash
	}

	if bytes.Equal(peerRoot, ourRoot) {
		entries := errnie.Guard(state, func() (SyncEntry_List, error) {
			return diff.NewEntries(0)
		})
		diff.SetEntries(entries)
		diff.SetMerkleRoot(ourRoot)
		diff.SetTerm(n.election.getCurrentTerm())
		diff.SetLogIndex(n.election.getLastLogIndex())
		return state.Err()
	}

	diffs := n.merkleTree.fullDiff(NewMerkleTree())

	entries := errnie.Guard(state, func() (SyncEntry_List, error) {
		return diff.NewEntries(int32(len(diffs)))
	})

	if state.Failed() {
		return state.Err()
	}

	fastest := n.forest.getFastestTree()

	for i, d := range diffs {
		entry := entries.At(i)
		entry.SetKey(d.Key)
		entry.SetTerm(n.election.getCurrentTerm())
		entry.SetIndex(n.election.getLastLogIndex() + uint64(i) + 1)

		value := d.Value
		if fastest != nil {
			if forestVal, ok := fastest.Get(d.Key); ok {
				value = forestVal
			}
		}

		entry.SetValue(value)
	}

	diff.SetEntries(entries)
	diff.SetMerkleRoot(ourRoot)
	diff.SetTerm(n.election.getCurrentTerm())
	diff.SetLogIndex(n.election.getLastLogIndex())

	return state.Err()
}

/*
Recover implements RadixRPC_Server.Recover.
It provides a mechanism for peers to recover their state after
failures or disconnections.
*/
func (n *NetworkNode) Recover(ctx context.Context, call RadixRPC_recover) error {
	// Similar to Sync but sends complete state
	return n.Sync(ctx, RadixRPC_sync(call))
}

/*
updateMerkleRoot updates the merkle root hash of the tree.
It rebuilds the merkle tree from the current state of the fastest tree
to ensure an accurate representation of the data.
*/
func (n *NetworkNode) updateMerkleRoot() {
	tree := n.forest.getFastestTree()
	if tree == nil {
		return
	}

	// Rebuild Merkle tree from current data
	it := tree.root.Root().Iterator()
	for key, value, ok := it.Next(); ok; key, value, ok = it.Next() {
		n.merkleTree.Insert(key, value)
	}
	n.merkleTree.Rebuild()
}

/*
BroadcastInsert broadcasts an insert operation to all connected peers.
It ensures data consistency by propagating insertions to all nodes
in the network.
*/
func (n *NetworkNode) BroadcastInsert(key []byte, value []byte) error {
	start := time.Now()

	currentTerm := n.election.getCurrentTerm()
	if currentTerm == 0 {
		return fmt.Errorf("dmt/network broadcast insert: missing term in request")
	}

	newLogIndex := n.election.getLastLogIndex() + 1

	n.election.updateLogState(newLogIndex, currentTerm)

	n.peersMutex.RLock()
	peers := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.peersMutex.RUnlock()

	for _, p := range peers {
		peer := p
		n.schedule("broadcast-"+peer.addr, func(ctx context.Context) (any, error) {
			state := errnie.NewState("dmt/network/broadcast")
			future, release := peer.client.Insert(n.ctx, func(p RadixRPC_insert_Params) error {
				if err := p.SetKey(key); err != nil {
					return err
				}
				if err := p.SetValue(value); err != nil {
					return err
				}
				p.SetTerm(currentTerm)
				p.SetLogIndex(newLogIndex)
				return nil
			})
			defer release()

			insertResult := errnie.Guard(state, future.Struct)

			if state.Failed() {
				return nil, state.Err()
			}

			if !insertResult.Success() {
				return nil, fmt.Errorf("remote insert rejected by %s", peer.addr)
			}

			return nil, state.Err()
		})
	}

	n.metrics.RecordInsert(time.Since(start), len(key)+len(value))

	return nil
}

/*
ListenAddr returns the resolved listen address including the actual port
assigned by the OS (useful when configured with port 0).
*/
func (n *NetworkNode) ListenAddr() string {
	if n.listener == nil {
		return ""
	}

	return n.listener.Addr().String()
}

/*
Close shuts down the network node.
It properly closes all peer connections and releases resources.
*/
func (n *NetworkNode) Close() error {
	n.cancel()
	n.election.Close()

	if n.listener != nil {
		errnie.GuardVoid(n.state, n.listener.Close)
	}

	n.peersMutex.Lock()
	defer n.peersMutex.Unlock()

	for _, p := range n.peers {
		errnie.GuardVoid(n.state, p.rpcConn.Close)
		errnie.GuardVoid(n.state, p.conn.Close)
	}

	return n.state.Err()
}

/*
GetMetrics returns the current metrics.
It provides a snapshot of the node's operational metrics including
peer count and other performance indicators.
*/
func (n *NetworkNode) GetMetrics() map[string]interface{} {
	n.peersMutex.RLock()
	n.metrics.UpdatePeerCount(int32(len(n.peers)))
	n.peersMutex.RUnlock()
	return n.metrics.GetMetrics()
}

/*
RequestVote implements RadixRPC_Server.RequestVote.
It handles vote requests during leader election, delegating the
decision to the election manager.
*/
func (n *NetworkNode) RequestVote(ctx context.Context, call RadixRPC_requestVote) error {
	state := errnie.NewState("dmt/network/request-vote")
	args := call.Args()
	term := args.Term()
	candidateId := errnie.Guard(state, args.CandidateId)
	lastLogIndex := args.LastLogIndex()
	lastLogTerm := args.LastLogTerm()

	if state.Failed() {
		return state.Err()
	}

	// Let election manager handle the vote request
	voteGranted := n.election.handleVoteRequest(term, candidateId, lastLogIndex, lastLogTerm)

	result := errnie.Guard(state, call.AllocResults)

	result.SetTerm(n.election.getCurrentTerm())
	result.SetVoteGranted(voteGranted)
	return state.Err()
}

/*
Heartbeat implements RadixRPC_Server.Heartbeat.
It processes heartbeat messages from the leader to maintain
the distributed system's consensus state.
*/
func (n *NetworkNode) Heartbeat(ctx context.Context, call RadixRPC_heartbeat) error {
	state := errnie.NewState("dmt/network/heartbeat")
	args := call.Args()
	term := args.Term()
	leaderId := errnie.Guard(state, args.LeaderId)

	if state.Failed() {
		return state.Err()
	}

	// Let election manager handle the heartbeat
	success := n.election.handleHeartbeat(term, leaderId)

	result := errnie.Guard(state, call.AllocResults)

	result.SetTerm(term)
	result.SetSuccess(success)
	return state.Err()
}

/*
schedule runs a NetworkNode background task on the Forest worker pool.
*/
func (n *NetworkNode) schedule(
	id string,
	fn func(ctx context.Context) (any, error),
) {
	n.forest.pool.Schedule(
		"dmt/network/"+id,
		pool.COMPUTE,
		&readPoolTask{ctx: n.ctx, fn: fn},
		pool.WithContext(n.ctx),
	)
}

func (n *NetworkNode) scheduleLoop(
	id string,
	fn func(ctx context.Context) (any, error),
) {
	n.forest.loops.Schedule(
		"dmt/network/"+id,
		pool.COMPUTE,
		&readPoolTask{ctx: n.ctx, fn: fn, loop: true},
		pool.WithContext(n.ctx),
		pool.WithTTL(time.Second),
	)
}
