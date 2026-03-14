package vm

import "github.com/theapemachine/six/pkg/system/pool"

/*
System is any component that participates in the broadcast message bus.
Systems announce themselves, then receive messages synchronously from
the Booter's event loop.

Receive(result) is called with a *pool.Result for broadcast messages.
Receive(nil) is a heartbeat/tick event; implementations must treat nil
as a tick rather than dereferencing.
*/
type System interface {
	Start(pool *pool.Pool, broadcast *pool.BroadcastGroup)
	Announce()
	Receive(result *pool.Result)
}
