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
Receive receives a value from the list of connections that this connection will listen to.
*/
func (conn *Conn) Receive(value *primitive.Value) {
	for _, inbox := range conn.inbox {
		inbox.Receive(value)
	}
}

/*
Broadcast broadcasts a value to the list of connections that this connection will listen to.
*/
func (conn *Conn) Broadcast(value *primitive.Value) {
	for _, outbox := range conn.outbox {
		outbox.Broadcast(value)
	}
}
