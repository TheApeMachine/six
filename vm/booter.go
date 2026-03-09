package vm

import (
	"context"
	"time"

	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/vm/cortex"
)

/*
System is any component that participates in the tick-based message bus.
Only the Booter runs a goroutine; systems receive messages synchronously.
*/
type System interface {
	Tick(result *pool.Result)
}

/*
CortexSystem wraps a cortex.Graph as a System.
Extracts pool.PoolValue from pool.Result for the cortex interface.
*/
type CortexSystem struct {
	graph *cortex.Graph
}

/*
Tick unwraps the broadcast Result into a PoolValue and forwards to cortex.
*/
func (cortexSystem *CortexSystem) Tick(result *pool.Result) {
	if result == nil || result.Value == nil {
		cortexSystem.graph.Tick(pool.PoolValue[any]{})
		return
	}

	if pv, ok := result.Value.(pool.PoolValue[any]); ok {
		cortexSystem.graph.Tick(pv)
		return
	}

	cortexSystem.graph.Tick(pool.PoolValue[any]{Value: result.Value})
}

/*
Booter is the single goroutine that owns the broadcast loop.
It routes messages to all registered systems and drives the tick clock.
*/
type Booter struct {
	ctx    context.Context
	cancel context.CancelFunc
	pool   *pool.Pool
}

type booterOpts func(*Booter)

/*
NewBooter instantiates a new Booter with the given options.
*/
func NewBooter(opts ...booterOpts) *Booter {
	booter := &Booter{}

	for _, opt := range opts {
		opt(booter)
	}

	return booter
}

/*
Start creates the broadcast group, wires systems, and runs the event loop.
Only this method spawns a goroutine; everything else runs inside pool.Schedule.
*/
func (booter *Booter) Start() {
	broadcast := booter.pool.CreateBroadcastGroup(
		"broadcast", 10*time.Second,
	)

	subscription := broadcast.Subscribe("booter", 128)

	machine := NewMachine(
		MachineWithPool(booter.pool),
		MachineWithBroadcast(broadcast),
	)

	graph := cortex.NewGraph(
		cortex.GraphWithContext(booter.ctx),
	)

	systems := []System{
		machine,
		&CortexSystem{graph: graph},
	}

	go func() {
		defer booter.cancel()

		for {
			select {
			case <-booter.ctx.Done():
				return
			case msg := <-subscription:
				for _, system := range systems {
					system.Tick(msg)
				}
			}
		}
	}()
}

/*
Stop terminates the Booter and signals all systems to finish.
*/
func (booter *Booter) Stop() {
	booter.cancel()
}

/*
BooterWithContext sets a cancellable context for the Booter lifecycle.
*/
func BooterWithContext(ctx context.Context) booterOpts {
	return func(booter *Booter) {
		booter.ctx, booter.cancel = context.WithCancel(ctx)
	}
}

/*
BooterWithPool injects the shared worker pool.
*/
func BooterWithPool(workerPool *pool.Pool) booterOpts {
	return func(booter *Booter) {
		booter.pool = workerPool
	}
}
