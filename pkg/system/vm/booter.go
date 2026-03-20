package vm

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	dmtserver "github.com/theapemachine/six/pkg/store/dmt/server"
	"github.com/theapemachine/six/pkg/system/cluster"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/system/vm/input"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Booter creates and owns all Cap'n Proto servers and vends their typed clients
to the Machine. It is the only place where server packages are imported;
the Machine never touches a server directly — it only calls clients.
*/
type Booter struct {
	state     *errnie.State
	ctx       context.Context
	cancel    context.CancelFunc
	pool      *pool.Pool
	broadcast *pool.BroadcastGroup
	router    *cluster.Router

	forest      *dmtserver.ForestServer
	tokenizer   *tokenizer.UniversalServer
	graph       *substrate.GraphServer
	macroIdx    *macro.MacroIndexServer
	hasSrv      *synthesis.HASServer
	prompter    *input.PrompterServer
	distributed *kernel.DistributedBackend
}

type booterOpts func(*Booter)

/*
NewBooter instantiates a Booter, starts all servers, and wires their clients.
*/
func NewBooter(opts ...booterOpts) *Booter {
	booter := &Booter{
		state: errnie.NewState("vm/booter"),
	}

	for _, opt := range opts {
		opt(booter)
	}

	errnie.GuardVoid(booter.state, func() error {
		return validate.Require(map[string]any{
			"ctx":       booter.ctx,
			"cancel":    booter.cancel,
			"pool":      booter.pool,
			"broadcast": booter.broadcast,
		})
	})

	booter.router = cluster.NewRouter(cluster.RouterWithContext(booter.ctx))

	booter.forest = dmtserver.NewForestServer(
		dmtserver.WithContext(booter.ctx),
		dmtserver.WithWorkerPool(booter.pool),
	)

	booter.tokenizer = tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(booter.ctx),
		tokenizer.UniversalWithPool(booter.pool),
	)

	booter.graph = substrate.NewGraphServer(
		substrate.GraphWithContext(booter.ctx),
		substrate.GraphWithWorkerPool(booter.pool),
		substrate.GraphWithRouter(booter.router),
	)

	booter.macroIdx = macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(booter.ctx),
	)

	booter.hasSrv = synthesis.NewHASServer(
		synthesis.HASWithContext(booter.ctx),
		synthesis.HASWithRouter(booter.router),
		synthesis.HASWithForest(booter.forest.Forest()),
		synthesis.HASWithMacroIndex(booter.macroIdx),
	)

	booter.prompter = input.NewPrompterServer(
		input.PrompterWithContext(booter.ctx),
	)

	booter.router.Register(cluster.FOREST, booter.forest)
	booter.router.Register(cluster.TOKENIZER, booter.tokenizer)
	booter.router.Register(cluster.GRAPH, booter.graph)
	booter.router.Register(cluster.HAS, booter.hasSrv)
	booter.router.Register(cluster.PROMPTER, booter.prompter)

	booter.distributed = errnie.Guard(booter.state, func() (*kernel.DistributedBackend, error) {
		return kernel.StartDistributed(
			booter.ctx,
			booter.router,
			config.System.Workers,
		)
	})

	errnie.GuardVoid(booter.state, func() error {
		return validate.Require(map[string]any{
			"router": booter.router,
		})
	})

	return booter
}

/*
Close releases all router-managed RPC clients and cancels the context.
*/
func (booter *Booter) Close() {
	if booter == nil {
		return
	}

	if booter.distributed != nil {
		_ = booter.distributed.Close()
	}

	if booter.prompter != nil {
		_ = booter.prompter.Close()
	}

	if booter.hasSrv != nil {
		_ = booter.hasSrv.Close()
	}

	if booter.graph != nil {
		_ = booter.graph.Close()
	}

	if booter.tokenizer != nil {
		_ = booter.tokenizer.Close()
	}

	if booter.forest != nil {
		_ = booter.forest.Close()
	}

	if booter.macroIdx != nil {
		_ = booter.macroIdx.Close()
	}

	if booter.router != nil {
		_ = booter.router.Close()
	}

	if booter.cancel != nil {
		booter.cancel()
	}
}

/*
BooterWithContext sets a cancellable context.
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
BooterError is a typed error for Booter failures.
*/
type BooterError string

const (
	ErrMissingRequirements BooterError = "booter: missing requirements"
)

func (err BooterError) Error() string {
	return string(err)
}
