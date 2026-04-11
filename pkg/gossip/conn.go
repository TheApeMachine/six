package gossip

type Conn struct {
	inbox  []*Conn
	outbox []*Conn
}

func NewConn(inbox []*Conn, outbox []*Conn) *Conn {
	return &Conn{
		inbox:  inbox,
		outbox: outbox,
	}
}

func (conn *Conn) Listen(others ...*Conn) {
	conn.inbox = append(conn.inbox, others...)
}
