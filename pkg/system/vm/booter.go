package vm

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	capnprpc "capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process"
)

/*
Booter is the single goroutine that owns the broadcast loop.
It routes messages to all registered systems and drives the tick clock.
*/
type Booter struct {
	ctx       context.Context
	cancel    context.CancelFunc
	pool      *pool.Pool
	broadcast *pool.BroadcastGroup
	systems   []System
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

	if booter.ctx == nil || booter.cancel == nil {
		booter.ctx, booter.cancel = context.WithCancel(context.Background())
	}

	return booter
}

/*
Start subscribes to the broadcast group, announces all systems, and
runs the event loop.
*/
func (booter *Booter) Start() {
	console.Info("Starting Booter")

	if booter.broadcast == nil {
		console.Error(ErrBooterNoBroadcast)
		return
	}

	subscription := booter.broadcast.Subscribe("broadcast", 128)

	for _, system := range booter.systems {
		system.Announce()
	}

	ticker := time.NewTicker(time.Millisecond * 10)
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
			return
		case msg := <-subscription:
			for _, system := range booter.systems {
				system.Receive(msg)
			}

			if pv, ok := msg.Value.(pool.PoolValue[net.Conn]); ok && pv.Key == "tokenizer" {
				console.Info("Tokenizer announced itself. Triggering dataset generation.")
				if err := booter.callGenerate(pv.Value); err != nil {
					console.Error(err)
				}
			}
		case <-ticker.C:
			for _, system := range booter.systems {
				system.Receive(nil)
			}
		}
	}
}

/*
callGenerate wraps the tokenizer's client-side net.Conn in a capnp RPC client
and calls Generate.
*/
func (booter *Booter) callGenerate(conn net.Conn) error {
	rpcConn := capnprpc.NewConn(capnprpc.NewStreamTransport(conn), nil)
	defer rpcConn.Close()

	client := process.Tokenizer(rpcConn.Bootstrap(booter.ctx))
	defer client.Release()

	if err := client.Generate(booter.ctx, nil); err != nil {
		return console.Error(err, "callGenerate", "client.Generate")
	}
	if err := client.WaitStreaming(); err != nil {
		return console.Error(err, "callGenerate", "client.WaitStreaming")
	}
	return nil
}

/*
Stop terminates the Booter and signals all systems to finish.
*/
func (booter *Booter) Stop() {
	if booter.cancel != nil {
		booter.cancel()
	}
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
BooterWithBroadcast injects a pre-created broadcast group.
*/
func BooterWithBroadcast(broadcast *pool.BroadcastGroup) booterOpts {
	return func(booter *Booter) {
		booter.broadcast = broadcast
	}
}

/*
BooterWithSystems injects the systems to wire into the broadcast loop.
*/
func BooterWithSystems(systems ...System) booterOpts {
	return func(booter *Booter) {
		booter.systems = systems
	}
}

/*
BooterError is a typed error for Booter failures.
*/
type BooterError string

const (
	ErrBooterNoBroadcast BooterError = "booter: broadcast is nil, cannot subscribe"
)

func (err BooterError) Error() string {
	return string(err)
}
