package vm

import (
	"context"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/grammar"
	"github.com/theapemachine/six/pkg/logic/semantic"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/console"
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
	ctx          context.Context
	cancel       context.CancelFunc
	pool         *pool.Pool
	broadcast    *pool.BroadcastGroup
	prompter     input.Prompter
	tok          tokenizer.Universal
	spatialIndex lsm.SpatialIndex
	graph        substrate.Graph
	engine       semantic.Engine
	parser       grammar.Parser
	cantilever   bvp.Cantilever
}

type booterOpts func(*Booter)

/*
NewBooter instantiates a Booter, starts all servers, and wires their clients.
*/
func NewBooter(opts ...booterOpts) *Booter {
	booter := &Booter{}

	for _, opt := range opts {
		opt(booter)
	}

	if errnie.Try(
		"system booting", validate.Require(map[string]any{
			"ctx":       booter.ctx,
			"cancel":    booter.cancel,
			"pool":      booter.pool,
			"broadcast": booter.broadcast,
		}),
	).Err() != nil {
		console.Error(ErrMissingRequirements)
		return nil
	}

	booter.prompter = input.NewPrompterServer(
		input.PrompterWithContext(booter.ctx),
	).Client("booter")

	booter.tok = tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(booter.ctx),
		tokenizer.UniversalWithPool(booter.pool),
	).Client("booter")

	booter.spatialIndex = lsm.NewSpatialIndexServer(
		lsm.WithContext(booter.ctx),
	).Client("booter")

	booter.graph = substrate.NewGraphServer(
		substrate.GraphWithContext(booter.ctx),
		substrate.GraphWithWorkerPool(booter.pool),
	).Client("booter")

	booter.engine = semantic.NewEngineServer(
		semantic.EngineWithContext(booter.ctx),
	).Client("booter")

	booter.parser = grammar.NewParserServer(
		grammar.ParserWithContext(booter.ctx),
	).Client("booter")

	// Pass arbitrary or default boundaries. Normally context manages this.
	booter.cantilever = bvp.NewCantileverServer(
		1, 1, bvp.CantileverWithContext(booter.ctx),
	).Client("booter")

	return booter
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
