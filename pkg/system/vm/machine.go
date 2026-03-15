package vm

import (
	"context"
	"runtime"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/grammar"
	"github.com/theapemachine/six/pkg/logic/semantic"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/system/vm/input"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Machine is the top-level orchestrator. All RPC result messages are kept alive
at Prompt scope so downstream calls can reference upstream data without copies.
Cap'n Proto's SetPtr does an internal deep-copy between messages as long as the
source segment is still alive — so the only contract is: don't release a result
until every consumer has fired its PlaceArgs callback.
*/
type Machine struct {
	ctx            context.Context
	cancel         context.CancelFunc
	workerPool     *pool.Pool
	broadcastGroup *pool.BroadcastGroup
	booter         *Booter
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{}

	for _, opt := range opts {
		opt(machine)
	}

	machine.workerPool = pool.New(
		machine.ctx,
		1,
		runtime.NumCPU(),
		&pool.Config{},
	)

	machine.broadcastGroup = pool.NewBroadcastGroup(
		"machine",
		5*time.Second,
		128,
	)

	errnie.SafeMustVoid(func() error {
		return validate.Require(map[string]any{
			"ctx":            machine.ctx,
			"cancel":         machine.cancel,
			"workerPool":     machine.workerPool,
			"broadcastGroup": machine.broadcastGroup,
		})
	})

	machine.booter = NewBooter(
		BooterWithContext(machine.ctx),
		BooterWithPool(machine.workerPool),
		BooterWithBroadcast(machine.broadcastGroup),
	)

	return machine
}

/*
Close shuts down the machine's booter, cancelling the context and
closing pipe-based RPC connections to prevent goroutine leaks.
*/
func (machine *Machine) Close() {
	machine.cancel()
	machine.booter.Close()
}

/*
Prompt is the main entry point. Every RPC result is deferred at this scope
so downstream PlaceArgs callbacks can safely read from upstream messages.
The final byte slice is copied into owned memory before returning because
capnp Data accessors return views into the segment — which gets freed by
the deferred releases.
*/
func (machine *Machine) Prompt(msg string) ([]byte, error) {
	ctx := machine.ctx

	// 1. Prompter — holdout masking
	promptFuture, promptRelease := machine.booter.prompter.Generate(
		ctx, func(p input.Prompter_generate_Params) error { return p.SetMsg(msg) },
	)
	defer promptRelease()

	promptResult := errnie.SafeMust(func() (input.Prompter_generate_Results, error) {
		return promptFuture.Struct()
	})

	promptBytes := errnie.SafeMust(func() ([]byte, error) {
		return promptResult.Data()
	})

	// 2. Tokenizer — bytes to edges
	tokFuture, tokRelease := machine.booter.tok.Generate(
		ctx, func(p tokenizer.Universal_generate_Params) error {
			return p.SetData(promptBytes)
		},
	)
	defer tokRelease()

	tokResult := errnie.SafeMust(func() (tokenizer.Universal_generate_Results, error) {
		return tokFuture.Struct()
	})

	edges := errnie.SafeMust(func() (lsm.GraphEdge_List, error) {
		return tokResult.Edges()
	})

	if edges.Len() == 0 {
		return nil, nil
	}

	// 3. SpatialIndex.Lookup — chord paths
	lookupFuture, lookupRelease := machine.booter.spatialIndex.Lookup(ctx, func(
		p lsm.SpatialIndex_lookup_Params,
	) error {
		chordList := errnie.Must(data.NewChord_List(p.Segment(), int32(edges.Len())))

		for i := range edges.Len() {
			dst := chordList.At(i)
			chord := errnie.Must(edges.At(i).Chord())
			dst.CopyFrom(chord)
		}

		errnie.MustVoid(p.SetChords(chordList))

		return nil
	})
	defer lookupRelease()

	lookupResult := errnie.SafeMust(func() (lsm.SpatialIndex_lookup_Results, error) {
		return lookupFuture.Struct()
	})

	paths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return lookupResult.Paths()
	})

	metaPaths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return lookupResult.MetaPaths()
	})

	// 4. Grammar + Semantic + BVP enrichment (best-effort, errors swallowed)
	machine.enrich(ctx, msg, paths)

	// 5. Graph.Prompt — recursive fold
	graphFuture, graphRelease := machine.booter.graph.Prompt(ctx, func(
		p substrate.Graph_prompt_Params,
	) error {
		errnie.MustVoid(p.SetPaths(paths))
		return p.SetMetaPaths(metaPaths)
	})
	defer graphRelease()

	graphResult := errnie.SafeMust(func() (substrate.Graph_prompt_Results, error) {
		return graphFuture.Struct()
	})

	resultPaths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return graphResult.Result()
	})

	// 6. SpatialIndex.Decode — chords back to bytes
	decodeFuture, decodeRelease := machine.booter.spatialIndex.Decode(ctx, func(
		p lsm.SpatialIndex_decode_Params,
	) error {
		return p.SetChords(resultPaths)
	})
	defer decodeRelease()

	decodeResult := errnie.SafeMust(func() (lsm.SpatialIndex_decode_Results, error) {
		return decodeFuture.Struct()
	})

	seqList := errnie.SafeMust(func() (capnp.DataList, error) {
		return decodeResult.Sequences()
	})

	if seqList.Len() == 0 {
		return nil, nil
	}

	result := errnie.SafeMust(func() ([]byte, error) {
		return seqList.At(0)
	})

	// seqList.At returns a view into the capnp segment. The deferred releases
	// will free that segment when Prompt returns, so we must own the bytes.
	out := make([]byte, len(result))
	copy(out, result)

	return out, nil
}

/*
enrich runs the grammar/semantic/bvp pipeline. Failures are non-fatal —
the spatial path alone is always sufficient for recall.
*/
func (machine *Machine) enrich(
	ctx context.Context,
	msg string,
	paths capnp.PointerList,
) {
	parseFuture, parseRelease := machine.booter.parser.Parse(
		ctx, func(p grammar.Parser_parse_Params) error {
			return p.SetMsg(msg)
		},
	)
	defer parseRelease()

	parseResult, err := parseFuture.Struct()
	if err != nil {
		return
	}

	promptPhase := numeric.Phase(parseResult.Phase())
	if promptPhase == 0 {
		return
	}

	subject, _ := parseResult.Subject()
	verb, _ := parseResult.Verb()
	object, _ := parseResult.Object()

	if subject == "" || verb == "" || object == "" {
		return
	}

	// Semantic query
	queryFuture, queryRelease := machine.booter.engine.Query(
		ctx, func(p semantic.Engine_query_Params) error {
			p.SetBraid(uint32(promptPhase))

			if err := p.SetKnownA(subject); err != nil {
				return err
			}

			if err := p.SetKnownB(verb); err != nil {
				return err
			}

			p.SetAxis(2)

			return nil
		},
	)
	defer queryRelease()

	queryFuture.Struct()

	// BVP bridge
	if paths.Len() == 0 || promptPhase == 0 {
		return
	}

	bridgeFuture, bridgeRelease := machine.booter.cantilever.Bridge(
		ctx, func(p bvp.Cantilever_bridge_Params) error {
			p.SetStart(1)
			p.SetGoal(uint32(promptPhase))
			return nil
		},
	)
	defer bridgeRelease()

	bridgeFuture.Struct()
}

/*
SetDataset loads a corpus into the machine. It reads all strings from the
dataset, ships the corpus to the Tokenizer via RPC (so the tokenizer has full
dataset context), then drives each string through Ingest to populate the
spatial index and semantic engine before any queries are served.
*/
func (machine *Machine) SetDataset(dataset provider.Dataset) error {
	byID := map[uint32][]byte{}

	for tok := range dataset.Generate() {
		byID[tok.SampleID] = append(byID[tok.SampleID], tok.Symbol)
	}

	corpus := make([]string, 0, len(byID))

	for _, bytes := range byID {
		corpus = append(corpus, string(bytes))
	}

	ctx := machine.ctx

	setFuture, setRelease := machine.booter.tok.SetDataset(ctx, func(
		p tokenizer.Universal_setDataset_Params,
	) error {
		list := errnie.SafeMust(func() (capnp.TextList, error) {
			return capnp.NewTextList(p.Segment(), int32(len(corpus)))
		})

		errnie.SafeMustVoid(func() error {
			return errnie.ForEach(len(corpus), func(i int) error {
				return list.Set(i, corpus[i])
			})
		})

		errnie.MustVoid(p.SetCorpus(list))

		return nil
	})

	errnie.SafeMust(func() (tokenizer.Universal_setDataset_Results, error) {
		return Call(setFuture, setRelease, func(
			s tokenizer.Universal_setDataset_Results,
		) (tokenizer.Universal_setDataset_Results, error) {
			return s, nil
		})
	})

	for _, item := range corpus {
		if err := machine.Ingest(item); err != nil {
			return err
		}
	}

	return nil
}

/*
Ingest generates edges from the string, populates the spatial index,
and injects S-V-O facts into the semantic engine via RPC.
All RPC results live at this scope so Insert can read from the tokenizer message.
*/
func (machine *Machine) Ingest(msg string) error {
	ctx := machine.ctx

	tokFuture, tokRelease := machine.booter.tok.Generate(ctx, func(
		p tokenizer.Universal_generate_Params,
	) error {
		return p.SetData([]byte(msg))
	})
	defer tokRelease()

	tokResult := errnie.SafeMust(func() (tokenizer.Universal_generate_Results, error) {
		return tokFuture.Struct()
	})

	edges := errnie.SafeMust(func() (lsm.GraphEdge_List, error) {
		return tokResult.Edges()
	})

	for i := range edges.Len() {
		edge := edges.At(i)

		errnie.SafeMustVoid(func() error {
			return machine.booter.spatialIndex.Insert(
				ctx, func(params lsm.SpatialIndex_insert_Params) error {
					return params.SetEdge(edge)
				},
			)
		})
	}

	errnie.SafeMustVoid(func() error {
		return machine.booter.spatialIndex.WaitStreaming()
	})

	machine.ingestSemantic(ctx, msg)

	return nil
}

/*
ingestSemantic attempts to parse the ingested string via Grammar Parser RPC
and, if successful, injects the resulting S-V-O fact into the Semantic Engine
via Inject RPC.
*/
func (machine *Machine) ingestSemantic(ctx context.Context, msg string) {
	parseFuture, parseRelease := machine.booter.parser.Parse(
		ctx, func(p grammar.Parser_parse_Params) error {
			return p.SetMsg(msg)
		},
	)
	defer parseRelease()

	parseResult, err := parseFuture.Struct()
	if err != nil || parseResult.Phase() == 0 {
		return
	}

	subject, _ := parseResult.Subject()
	verb, _ := parseResult.Verb()
	object, _ := parseResult.Object()

	if subject == "" || verb == "" || object == "" {
		return
	}

	injectFuture, injectRelease := machine.booter.engine.Inject(
		ctx, func(p semantic.Engine_inject_Params) error {
			if err := p.SetSubject(subject); err != nil {
				return err
			}

			if err := p.SetLink(verb); err != nil {
				return err
			}

			return p.SetObject(object)
		},
	)
	defer injectRelease()

	injectFuture.Struct()
}

/*
MachineWithContext adds a context to the Machine.
*/
func MachineWithContext(ctx context.Context) machineOpts {
	return func(machine *Machine) {
		machine.ctx, machine.cancel = context.WithCancel(ctx)
	}
}

/*
MachineError is a typed error for Machine failures.
*/
type MachineError string

const (
	ErrMachineMissingRequirements MachineError = "machine: missing requirements"
)

func (machineError MachineError) Error() string {
	return string(machineError)
}
