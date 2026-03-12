package vm

import (
	"context"
	"time"

	"github.com/theapemachine/six/pkg/logic/graph"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/store/lsm"
)

/*
System is any component that participates in the broadcast message bus.
Only the Booter runs a goroutine; systems receive messages synchronously.
*/
type System interface {
	Receive(result *pool.Result)
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
This method spawns a single goroutine to manage the event loop.
*/
func (booter *Booter) Start() {
	broadcast := booter.pool.CreateBroadcastGroup(
		"broadcast", 10*time.Second,
	)

	subscription := broadcast.Subscribe("booter", 128)

	index := lsm.NewSpatialIndexServer(
		lsm.WithContext(booter.ctx),
		lsm.WithBroadcast(broadcast),
	)

	tokenizer := process.NewTokenizerServer(
		process.TokenizerWithContext(booter.ctx),
		process.TokenizerWithPool(booter.pool),
		process.TokenizerWithBroadcast(broadcast),
	)

	matrix := graph.NewMatrixServer(
		graph.MatrixWithContext(booter.ctx),
		graph.MatrixWithPool(booter.pool),
		graph.MatrixWithBroadcast(broadcast),
	)

	systems := []System{
		index,
		tokenizer,
		matrix,
	}

	index.Announce()
	tokenizer.Announce()
	matrix.Announce()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-booter.ctx.Done():
			return
		case msg := <-subscription:
			for _, system := range systems {
				system.Receive(msg)
			}
		case <-ticker.C:
			for _, system := range systems {
				system.Receive(nil)
			}
		}
	}
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
