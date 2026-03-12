package vm

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	capnprpc "capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/logic/graph"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/provider"
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
	ctx     context.Context
	cancel  context.CancelFunc
	pool    *pool.Pool
	dataset provider.Dataset
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
	console.Info("Starting Booter")

	broadcast := booter.pool.CreateBroadcastGroup(
		"broadcast", 10*time.Second,
	)

	subscription := broadcast.Subscribe("broadcast", 128)

	index := lsm.NewSpatialIndexServer(
		lsm.WithContext(booter.ctx),
		lsm.WithBroadcast(broadcast),
	)

	tokenizer := process.NewTokenizerServer(
		process.TokenizerWithContext(booter.ctx),
		process.TokenizerWithPool(booter.pool),
		process.TokenizerWithBroadcast(broadcast),
		process.TokenizerWithDataset(booter.dataset),
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	for {
		select {
		case <-booter.ctx.Done():
			return
		case <-sig:
			console.Info("Signal received by Booter. Shutting down...")
			booter.Stop()
			os.Exit(0)
		case msg := <-subscription:
			for _, system := range systems {
				system.Receive(msg)
			}

			// When the tokenizer announces itself, trigger dataset generation via RPC.
			if booter.dataset != nil {
				if pv, ok := msg.Value.(pool.PoolValue[net.Conn]); ok && pv.Key == "tokenizer" {
					console.Info("Tokenizer announced itself. Triggering dataset generation.")
					booter.callGenerate(pv.Value)
				}
			}
		case <-ticker.C:
			for _, system := range systems {
				system.Receive(nil)
			}
		}
	}
}

/*
callGenerate wraps the tokenizer's client-side net.Conn in a capnp RPC client
and calls Generate. The dataset is already injected into the server, so no raw
bytes are needed — the call is just the trigger.
*/
func (booter *Booter) callGenerate(conn net.Conn) {
	rpcConn := capnprpc.NewConn(capnprpc.NewStreamTransport(conn), nil)
	defer rpcConn.Close()

	client := process.Tokenizer(rpcConn.Bootstrap(booter.ctx))
	defer client.Release()

	_ = client.Generate(booter.ctx, nil)
	_ = client.WaitStreaming()
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

/*
BooterWithDataset injects a dataset. The Booter will begin loading it
automatically after the tokenizer announces itself on the broadcast bus.
*/
func BooterWithDataset(dataset provider.Dataset) booterOpts {
	return func(booter *Booter) {
		booter.dataset = dataset
	}
}
