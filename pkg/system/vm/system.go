package vm

import (
	"context"
	"net"

	"github.com/theapemachine/six/pkg/store/data"
)

/*
System is any component that participates in the broadcast message bus.
Systems announce themselves, then receive messages synchronously from
the Booter's event loop.

Receive(result) is called with a *pool.Result for broadcast messages.
Receive(nil) is a heartbeat/tick event; implementations must treat nil
as a tick rather than dereferencing.
*/
type System interface {
	Client(string) net.Conn
}

/*
Promptable is any system that can be prompted.
*/
type Promptable interface {
	Prompt(
		ctx context.Context, msg []data.Chord,
	) (buf []data.Chord, err error)
}
