package vm

import (
	"context"
	"io"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/dmt/server"
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
	state        *errnie.State
	ctx          context.Context
	cancel       context.CancelFunc
	pool         *pool.Pool
	broadcast    *pool.BroadcastGroup
	prompter     input.Prompter
	tok          tokenizer.Universal
	forestClient server.Server
	graph        substrate.Graph
	graphServer  *substrate.GraphServer
	cantilever   bvp.Cantilever
	has          synthesis.HAS
	sharedIndex  *macro.MacroIndexServer
	closers      []io.Closer
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

	promptServer := input.NewPrompterServer(
		input.PrompterWithContext(booter.ctx),
	)
	booter.prompter = promptServer.Client("booter")

	/*
		ForestServer acts as the primary topological memory substrate.
		It is initialized early so that higher-level services like the
		Tokenizer can use it for sequence persistence.
	*/
	forestServer := server.NewForestServer(
		server.WithContext(booter.ctx),
		server.WithWorkerPool(booter.pool),
	)
	booter.forestClient = forestServer.Client("booter")

	tokServer := tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(booter.ctx),
		tokenizer.UniversalWithPool(booter.pool),
	)
	booter.tok = tokServer.Client("booter")

	sharedIndex := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(booter.ctx),
	)
	booter.sharedIndex = sharedIndex

	graphServer := substrate.NewGraphServer(
		substrate.GraphWithContext(booter.ctx),
		substrate.GraphWithWorkerPool(booter.pool),
		substrate.GraphWithMacroIndex(sharedIndex),
	)
	booter.graphServer = graphServer
	booter.graph = graphServer.Client("booter")

	cantileverServer := bvp.NewCantileverServer(
		bvp.CantileverWithContext(booter.ctx),
		bvp.WithMacroIndex(sharedIndex),
	)
	booter.cantilever = cantileverServer.Client("booter")

	hasServer := synthesis.NewHASServer(
		synthesis.HASWithContext(booter.ctx),
		synthesis.HASWithMacroIndex(sharedIndex),
		synthesis.HASWithForest(forestServer.Forest()),
	)
	booter.has = hasServer.Client("booter")

	booter.closers = []io.Closer{
		hasServer,
		cantileverServer,
		sharedIndex,
		graphServer,
		tokServer,
		forestServer,
		promptServer,
	}

	return booter
}

/*
Close cancels the context and closes pipe-based RPC servers.
*/
func (booter *Booter) Close() {
	booter.cancel()

	for _, closer := range booter.closers {
		closer.Close()
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
