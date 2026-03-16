package vm

import (
	"context"
	"runtime"
	"sort"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/compute/kernel"
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
	"github.com/theapemachine/six/pkg/telemetry"
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
	projection     ProjectionMode
	booter         *Booter
	sink           *telemetry.Sink
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		sink: telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.ctx == nil || machine.cancel == nil {
		machine.ctx, machine.cancel = context.WithCancel(context.Background())
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
		BooterWithProjection(machine.projection),
	)

	// Start distributed worker and discovery
	kernel.StartDiscovery(machine.ctx, ":7777")

	return machine
}

/*
Close shuts down the machine's booter, cancelling the context and
closing pipe-based RPC connections to prevent goroutine leaks.
*/
func (machine *Machine) Close() {
	if machine.booter != nil {
		machine.booter.Close()
	}
	if machine.broadcastGroup != nil {
		machine.broadcastGroup.Close()
	}
	if machine.workerPool != nil {
		machine.workerPool.Close()
	}
	if machine.cancel != nil {
		machine.cancel()
	}
}

func keyListToSlice(list capnp.UInt64List) []uint64 {
	keys := make([]uint64, list.Len())
	for i := 0; i < list.Len(); i++ {
		keys[i] = list.At(i)
	}
	return keys
}

func valueListFromSlice(seg *capnp.Segment, values []data.Value) (data.Value_List, error) {
	valueList, err := data.NewValue_List(seg, int32(len(values)))
	if err != nil {
		return data.Value_List{}, err
	}

	for i, value := range values {
		dst := valueList.At(i)
		dst.CopyFrom(value)
	}

	return valueList, nil
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

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data:      telemetry.EventData{Stage: "prompt-start", Msg: msg},
	})

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

	// 2. Tokenizer — bytes to radix keys, then projection into observable prompt values
	tokFuture, tokRelease := machine.booter.tok.Generate(
		ctx, func(p tokenizer.Universal_generate_Params) error {
			return p.SetData(promptBytes)
		},
	)
	defer tokRelease()

	tokResult := errnie.SafeMust(func() (tokenizer.Universal_generate_Results, error) {
		return tokFuture.Struct()
	})

	keyList := errnie.SafeMust(func() (capnp.UInt64List, error) {
		return tokResult.Keys()
	})
	keys := keyListToSlice(keyList)
	promptValues := data.CompileObservableSequenceValues(keys)

	machine.sink.Emit(telemetry.Event{
		Component: "Tokenizer",
		Action:    "Value",
		Data: telemetry.EventData{
			Stage:     "tokenize",
			EdgeCount: len(promptValues),
			ChunkText: msg,
		},
	})

	if len(promptValues) == 0 {
		return nil, nil
	}

	// 3. SpatialIndex.Lookup — projected prompt values in, native paths out
	lookupFuture, lookupRelease := machine.booter.spatialIndex.Lookup(ctx, func(
		p lsm.SpatialIndex_lookup_Params,
	) error {
		valueList, err := valueListFromSlice(p.Segment(), promptValues)
		if err != nil {
			return err
		}

		errnie.MustVoid(p.SetValues(valueList))

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

	machine.sink.Emit(telemetry.Event{
		Component: "SpatialIndex",
		Action:    "Lookup",
		Data: telemetry.EventData{
			Stage:     "lookup",
			PathCount: paths.Len(),
			Msg:       msg,
		},
	})

	// 4. Optional projection overlay (best-effort, errors swallowed)
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

	machine.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "Evaluate",
		Data: telemetry.EventData{
			Stage:     "fold",
			PathCount: resultPaths.Len(),
		},
	})

	// 6. SpatialIndex.Decode — values back to bytes
	decodeFuture, decodeRelease := machine.booter.spatialIndex.Decode(ctx, func(
		p lsm.SpatialIndex_decode_Params,
	) error {
		return p.SetValues(resultPaths)
	})
	defer decodeRelease()

	decodeResult := errnie.SafeMust(func() (lsm.SpatialIndex_decode_Results, error) {
		return decodeFuture.Struct()
	})

	seqList := errnie.SafeMust(func() (capnp.DataList, error) {
		return decodeResult.Sequences()
	})

	if seqList.Len() == 0 {
		machine.sink.Emit(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data:      telemetry.EventData{Stage: "prompt-empty", Msg: msg},
		})

		return nil, nil
	}

	result := errnie.SafeMust(func() ([]byte, error) {
		return seqList.At(0)
	})

	// seqList.At returns a view into the capnp segment. The deferred releases
	// will free that segment when Prompt returns, so we must own the bytes.
	out := make([]byte, len(result))
	copy(out, result)

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:      "prompt-complete",
			Msg:        msg,
			ResultText: string(out),
		},
	})

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
	if !machine.projection.Enabled(ProjectionPrompt) {
		return
	}

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
	type sampleSymbol struct {
		pos    uint32
		symbol byte
	}

	byID := map[uint32][]sampleSymbol{}

	for tok := range dataset.Generate() {
		byID[tok.SampleID] = append(byID[tok.SampleID], sampleSymbol{
			pos:    tok.Pos,
			symbol: tok.Symbol,
		})
	}

	ids := make([]uint32, 0, len(byID))
	for sampleID := range byID {
		ids = append(ids, sampleID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})

	corpus := make([]string, 0, len(byID))
	for _, sampleID := range ids {
		symbols := append([]sampleSymbol(nil), byID[sampleID]...)
		sort.Slice(symbols, func(i, j int) bool {
			if symbols[i].pos != symbols[j].pos {
				return symbols[i].pos < symbols[j].pos
			}
			return symbols[i].symbol < symbols[j].symbol
		})

		bytes := make([]byte, len(symbols))
		for i, symbol := range symbols {
			bytes[i] = symbol.symbol
		}

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
Ingest tokenizes the string into radix keys, compiles those observations into
native value-plane program cells, populates the spatial index, and injects S-V-O
facts into the semantic engine via RPC.
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

	keyList := errnie.SafeMust(func() (capnp.UInt64List, error) {
		return tokResult.Keys()
	})
	keys := keyListToSlice(keyList)
	cells := data.CompileSequenceCells(keys)

	machine.sink.Emit(telemetry.Event{
		Component: "Tokenizer",
		Action:    "Value",
		Data: telemetry.EventData{
			Stage:     "ingest-tokenize",
			EdgeCount: len(cells),
			ChunkText: msg,
		},
	})

	for _, cell := range cells {
		compiledCell := cell

		errnie.SafeMustVoid(func() error {
			return machine.booter.spatialIndex.Insert(
				ctx, func(params lsm.SpatialIndex_insert_Params) error {
					edge, err := params.NewEdge()
					if err != nil {
						return err
					}

					edge.SetLeft(compiledCell.Symbol)
					edge.SetRight(compiledCell.NextSymbol)
					edge.SetPosition(compiledCell.Position)

					value, err := edge.NewValue()
					if err != nil {
						return err
					}
					value.CopyFrom(compiledCell.Value)

					meta, err := edge.NewMeta()
					if err != nil {
						return err
					}
					meta.CopyFrom(compiledCell.Meta)

					return nil
				},
			)
		})
	}

	errnie.SafeMustVoid(func() error {
		return machine.booter.spatialIndex.WaitStreaming()
	})

	machine.sink.Emit(telemetry.Event{
		Component: "LSM",
		Action:    "Insert",
		Data: telemetry.EventData{
			Stage:     "ingest-insert",
			Edges:     len(cells),
			ChunkText: msg,
		},
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
	if !machine.projection.Enabled(ProjectionIngest) {
		return
	}

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
MachineWithProjection enables the human-facing projection overlay. Projection is
disabled by default so the core Machine boots only the native storage and
wavefront substrate unless experiments explicitly ask for grammar/semantic help.
*/
func MachineWithProjection(mode ProjectionMode) machineOpts {
	return func(machine *Machine) {
		machine.projection = mode
	}
}

/*
MachineWithContext adds a context to the Machine.
*/
func MachineWithContext(ctx context.Context) machineOpts {
	return func(machine *Machine) {
		if ctx == nil {
			ctx = context.Background()
		}
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
