package gossip

import "github.com/theapemachine/six/pkg/primitive"

/*
GossipConn is an interface that must be implemented by any type
that wants to participate in the gossip protocol.
*/
type GossipConn interface {
	Listen(others ...GossipConn)
	Receive(value *primitive.Value)
	Broadcast(value *primitive.Value)
}

/*
Conn is a connection that can be used to participate in the gossip protocol.
*/
type Conn struct {
	inbox  []GossipConn
	outbox []GossipConn
}

/*
NewConn creates a new connection that can be used to participate in the gossip protocol.
*/
func NewConn(inbox []GossipConn, outbox []GossipConn) *Conn {
	return &Conn{
		inbox:  inbox,
		outbox: outbox,
	}
}

/*
Listen adds a new connection to the list of connections that this connection will listen to.
*/
func (conn *Conn) Listen(others ...GossipConn) {
	conn.inbox = append(conn.inbox, others...)
}

/*
Receive forwards a value to each non-nil entry in conn.inbox (GossipConn.Receive).
Cycles among *Conn nodes are avoided by tracking visited *Conn values in a map;
Receive starts a fresh propagation (nil visited set). Non-*Conn peers are invoked
without that tracking. See ReceiveWithVisited for explicit visited reuse.
*/
func (conn *Conn) Receive(value *primitive.Value) {
	conn.receiveWithVisited(nil, value)
}

/*
ReceiveWithVisited forwards value along conn.inbox like Receive but reuses visited
so a message is not forwarded twice through the same *Conn (prevents stack overflow
when inbox wiring forms a cycle). Pass visited == nil to begin a new trace; the
current Conn is marked in visited before any recursive forwarding to peers.
*/
func (conn *Conn) ReceiveWithVisited(visited map[*Conn]struct{}, value *primitive.Value) {
	conn.receiveWithVisited(visited, value)
}

func (conn *Conn) receiveWithVisited(visited map[*Conn]struct{}, value *primitive.Value) {
	if conn == nil {
		return
	}

	if visited == nil {
		visited = make(map[*Conn]struct{})
	}

	if _, seen := visited[conn]; seen {
		return
	}

	visited[conn] = struct{}{}

	for _, inbox := range conn.inbox {
		if inbox == nil {
			continue
		}

		if peer, ok := inbox.(*Conn); ok {
			peer.receiveWithVisited(visited, value)

			continue
		}

		inbox.Receive(value)
	}
}

/*
Broadcast sends the value to each non-nil peer in conn.outbox (not the inbox list).
For *Conn peers, the same visited map used for Receive prevents re-entering a node
already handled in this propagation, so cyclic outbox wiring does not re-broadcast
indefinitely. Non-*Conn outbox entries get Broadcast(value) without visited tracking.
*/
func (conn *Conn) Broadcast(value *primitive.Value) {
	conn.broadcastWithVisited(nil, value)
}

func (conn *Conn) broadcastWithVisited(visited map[*Conn]struct{}, value *primitive.Value) {
	if conn == nil {
		return
	}

	if visited == nil {
		visited = make(map[*Conn]struct{})
	}

	if _, seen := visited[conn]; seen {
		return
	}

	visited[conn] = struct{}{}

	for _, outbox := range conn.outbox {
		if outbox == nil {
			continue
		}

		if peer, ok := outbox.(*Conn); ok {
			peer.broadcastWithVisited(visited, value)

			continue
		}

		outbox.Broadcast(value)
	}
}
