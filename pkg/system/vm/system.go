package vm

import "github.com/theapemachine/six/pkg/system/pool"

/*
System is any component that participates in the broadcast message bus.
Systems announce themselves, then receive messages synchronously from
the Booter's event loop.
*/
type System interface {
	Start(pool *pool.Pool, broadcast *pool.BroadcastGroup)
	Announce()
	Receive(result *pool.Result)
}
