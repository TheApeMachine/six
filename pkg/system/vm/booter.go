package vm

import (
	"context"
	"io"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/grammar"
	"github.com/theapemachine/six/pkg/logic/semantic"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/lsm"
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
	closers      []io.Closer
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

	errnie.SafeMustVoid(func() error {
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

	tokServer := tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(booter.ctx),
		tokenizer.UniversalWithPool(booter.pool),
	)
	booter.tok = tokServer.Client("booter")

	spatialServer := lsm.NewSpatialIndexServer(
		lsm.WithContext(booter.ctx),
	)
	booter.spatialIndex = spatialServer.Client("booter")

	graphServer := substrate.NewGraphServer(
		substrate.GraphWithContext(booter.ctx),
		substrate.GraphWithWorkerPool(booter.pool),
	)
	booter.graph = graphServer.Client("booter")

	engineServer := semantic.NewEngineServer(
		semantic.EngineWithContext(booter.ctx),
	)
	booter.engine = engineServer.Client("booter")

	parserServer := grammar.NewParserServer(
		grammar.ParserWithContext(booter.ctx),
		grammar.ParserWithNouns(
			"mary", "john", "daniel", "sandra",
			"bathroom", "bedroom", "kitchen", "hallway",
			"garden", "office", "cat", "dog", "bird",
			"mat", "yard", "wall", "sky", "fish",
		),
		grammar.ParserWithVerbs(
			"moved", "went", "journeyed", "travelled",
			"is_on", "is_in", "flew", "flew_over",
			"likes", "rel",
		),
		grammar.ParserWithStopwords(
			"to", "the", "a", "an", "is", "in", "on",
			"where", "what", "who", "how",
		),
	)
	booter.parser = parserServer.Client("booter")

	sharedIndex := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(booter.ctx),
	)

	cantileverServer := bvp.NewCantileverServer(
		bvp.CantileverWithContext(booter.ctx),
		bvp.WithMacroIndex(sharedIndex),
	)
	booter.cantilever = cantileverServer.Client("booter")

	booter.closers = []io.Closer{
		promptServer,
		tokServer,
		spatialServer,
		graphServer,
		engineServer,
		parserServer,
		sharedIndex,
		cantileverServer,
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
