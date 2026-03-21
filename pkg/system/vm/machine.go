package vm

import (
	"context"
	"fmt"
	"runtime"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/dmt/server"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Machine is the top-level orchestrator. All RPC result
messages are kept alive at Prompt scope so downstream
calls can reference upstream data without copies.
*/
type Machine struct {
	state          *errnie.State
	ctx            context.Context
	cancel         context.CancelFunc
	workerPool     *pool.Pool
	broadcastGroup *pool.BroadcastGroup
	booter         *Booter
	sink           *telemetry.Sink
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		state: errnie.NewState("vm/machine"),
		sink:  telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.ctx == nil || machine.cancel == nil {
		machine.ctx, machine.cancel = context.WithCancel(
			context.Background(),
		)
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

	errnie.GuardVoid(machine.state, func() error {
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
	if machine.booter != nil {
		_ = machine.booter.Close()
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

/*
Prompt tokenizes the prompt, runs the graph-local prompt wavefront, and decodes
the exact continuation returned by the graph.
*/
func (machine *Machine) Prompt(msg string) ([]byte, error) {
	machine.state.Reset()
	ctx := machine.ctx

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:   "prompt-start",
			Message: msg,
		},
	})

	out := errnie.Guard(machine.state, func() ([]byte, error) {
		return machine.runPromptThroughBackend(ctx, msg)
	})

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:      "prompt-complete",
			Message:    msg,
			ResultText: string(out),
		},
	})

	return out, nil
}

/*
runPromptThroughBackend schedules one prompt/answer cycle on the worker pool and
drains the result through a transport pipeline backed by compute.Backend.
*/
func (machine *Machine) runPromptThroughBackend(
	ctx context.Context,
	msg string,
) ([]byte, error) {
	raw, err := machine.booter.router.Get(ctx, cluster.CANTILEVER, "machine")
	if err != nil {
		return nil, err
	}

	route := NewCantileverRoute(ctx, raw, bvp.Cantilever(raw))
	backend, err := compute.NewBackend(
		compute.BackendWithOperations(
			route,
		),
	)
	if err != nil {
		raw.Release()
		return nil, err
	}

	task := NewPromptTask(
		[]byte(msg),
		backend,
		route,
	)
	jobID := fmt.Sprintf("machine/prompt/%d", time.Now().UnixNano())

	if err := machine.workerPool.Schedule(
		jobID,
		pool.COMPUTE,
		task,
		pool.WithContext(ctx),
	); err != nil {
		return nil, err
	}

	return machine.awaitPromptResult(ctx, jobID)
}

/*
awaitPromptResult waits for the scheduled prompt job to publish its result.
*/
func (machine *Machine) awaitPromptResult(
	ctx context.Context,
	jobID string,
) ([]byte, error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		result, ok := machine.workerPool.StoredResult(jobID)
		if ok {
			if result.Error != nil {
				return nil, result.Error
			}

			if bytes, ok := result.Value.([]byte); ok {
				return bytes, nil
			}

			if result.Value == nil {
				return nil, nil
			}

			return nil, fmt.Errorf("prompt result has unexpected type %T", result.Value)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

/*
SetDataset streams the raw dataset bytes through the tokenizer RPC as a
continuous flow. The sequencer discovers boundaries internally; the resulting
Morton keys encode boundary-local depth which resets at each sequencer cut.
We split the key stream at depth resets to produce per-sequence Values for
the Graph AST.
*/
func (machine *Machine) SetDataset(dataset provider.Dataset) error {
	ctx := machine.ctx
	rows := make([][]byte, 0, 8)
	currentRow := make([]byte, 0, 64)
	currentSampleID := ^uint32(0)

	tokClient := errnie.Guard(machine.state, func() (tokenizer.Universal, error) {
		raw, err := machine.booter.router.Get(ctx, cluster.TOKENIZER, "machine")
		return tokenizer.Universal(raw), err
	})

	for tok := range dataset.Generate() {
		if currentSampleID != ^uint32(0) && tok.SampleID != currentSampleID {
			rows = append(rows, append([]byte(nil), currentRow...))
			currentRow = currentRow[:0]
		}

		currentSampleID = tok.SampleID
		currentRow = append(currentRow, tok.Symbol)

		errnie.GuardVoid(machine.state, func() error {
			return tokClient.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(tok.Symbol)
					return nil
				},
			)
		})
	}

	if len(currentRow) > 0 {
		rows = append(rows, append([]byte(nil), currentRow...))
	}

	errnie.GuardVoid(machine.state, func() error {
		return tokClient.WaitStreaming()
	})

	keys := errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	drained := errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	keys = append(keys, drained...)

	if machine.state.Failed() {
		return machine.state.Err()
	}

	values, _ := compilePromptSequence(keys)
	errnie.GuardVoid(machine.state, func() error {
		return machine.storePromptCorpusRows(rows)
	})
	machine.ingestHASBoundaries(ctx, values)

	machine.emitTokenizerTelemetryFromKeys(keys, "ingest-tokenize")
	machine.writeKeys(ctx, keys)

	errnie.GuardVoid(machine.state, func() error {
		raw, err := machine.booter.router.Get(ctx, cluster.GRAPH, "machine")

		if err != nil {
			return err
		}

		future, release := substrate.Graph(raw).Done(ctx, nil)
		defer release()
		_, err = future.Struct()
		return err
	})

	return machine.state.Err()
}

/*
storePromptCorpusRows materializes exact prompt rows through a fresh tokenizer
path per sample so query-time prompt values match the stored corpus shape.
*/
func (machine *Machine) storePromptCorpusRows(rows [][]byte) error {
	compiled := make([][]primitive.Value, 0, len(rows))

	for _, row := range rows {
		values, err := machine.compilePromptRow(row)
		if err != nil {
			return err
		}

		if len(values) == 0 {
			continue
		}

		compiled = append(compiled, values)
	}

	machine.booter.cantilever.Store(compiled)

	return nil
}

/*
compilePromptRow runs one sample through a fresh tokenizer server and returns
the compiled observable row used by exact prompt lookup.
*/
func (machine *Machine) compilePromptRow(raw []byte) ([]primitive.Value, error) {
	server := tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(machine.ctx),
		tokenizer.UniversalWithPool(machine.workerPool),
	)
	defer server.Close()

	client := tokenizer.Universal(server.Client("prompt-row"))

	for _, symbol := range raw {
		if err := client.Write(
			machine.ctx, func(params tokenizer.Universal_write_Params) error {
				params.SetData(symbol)
				return nil
			},
		); err != nil {
			return nil, err
		}
	}

	if err := client.WaitStreaming(); err != nil {
		return nil, err
	}

	keys, err := machine.tokenizerKeys(machine.ctx, client)
	if err != nil {
		return nil, err
	}

	return primitive.CompileObservableSequenceValues(keys), nil
}

/*
tokenizerKeys drains a tokenizer client in the same two-pass pattern used by prompt execution.
*/
func (machine *Machine) tokenizerKeys(
	ctx context.Context,
	client tokenizer.Universal,
) ([]uint64, error) {
	keys := make([]uint64, 0)

	for range 2 {
		future, release := client.Done(ctx, nil)
		results, err := future.Struct()
		if err != nil {
			release()
			return nil, err
		}

		list, err := results.Keys()
		if err != nil {
			release()
			return nil, err
		}

		for index := range list.Len() {
			keys = append(keys, list.At(index))
		}

		release()
	}

	return keys, nil
}

/*
ingestHASBoundaries feeds adjacent value boundaries to HAS so ingestion-time
tool synthesis is wired into the runtime pipeline.
*/
func (machine *Machine) ingestHASBoundaries(ctx context.Context, values []primitive.Value) {
	if machine.booter == nil || machine.booter.router == nil || len(values) < 2 {
		return
	}

	for index := 1; index < len(values); index++ {
		startValue := values[index-1]
		endValue := values[index]

		errnie.GuardVoid(machine.state, func() error {
			return machine.emitHASResult(ctx, startValue, endValue, "dataset")
		})

		if machine.state.Failed() {
			return
		}
	}
}

/*
emitHASResult writes one boundary pair to HAS, finalizes synthesis, and emits a
plain-text result line that can be inspected in runtime logs.
*/
func (machine *Machine) emitHASResult(
	ctx context.Context,
	startValue primitive.Value,
	endValue primitive.Value,
	source string,
) error {
	raw, err := machine.booter.router.Get(ctx, cluster.HAS, "machine")

	if err != nil {
		return err
	}

	has := synthesis.HAS(raw)

	if err := has.Write(ctx, func(params synthesis.HAS_write_Params) error {
		if err := params.SetStart(startValue); err != nil {
			return err
		}

		return params.SetEnd(endValue)
	}); err != nil {
		return err
	}

	if err := has.WaitStreaming(); err != nil {
		return err
	}

	doneFuture, release := has.Done(ctx, nil)
	defer release()

	doneResult, err := doneFuture.Struct()

	if err != nil {
		return err
	}

	keyText, err := doneResult.KeyText()
	if err != nil {
		return err
	}

	line := fmt.Sprintf(
		"HAS %s result: key=%s useCount=%d hardened=%t winner=%d residue=%d steps=%d",
		source,
		keyText,
		doneResult.UseCount(),
		doneResult.Hardened(),
		doneResult.WinnerIndex(),
		doneResult.PostResidue(),
		doneResult.Steps(),
	)

	console.Trace(line)

	return nil
}

/*
tokenizeStream feeds bytes through the tokenizer and returns the exact keys.
*/
func (machine *Machine) tokenizeStream(raw []byte) ([]uint64, error) {
	ctx := machine.ctx
	keys := make([]uint64, 0, len(raw))

	tokClient := errnie.Guard(machine.state, func() (tokenizer.Universal, error) {
		raw, err := machine.booter.router.Get(ctx, cluster.TOKENIZER, "machine")
		return tokenizer.Universal(raw), err
	})

	for _, symbol := range raw {
		errnie.GuardVoid(machine.state, func() error {
			return tokClient.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(symbol)
					return nil
				},
			)
		})
	}

	errnie.GuardVoid(machine.state, func() error {
		return tokClient.WaitStreaming()
	})

	drained := errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	keys = append(keys, drained...)

	drained = errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	keys = append(keys, drained...)

	machine.emitTokenizerTelemetryFromKeys(keys, "tokenize")

	return keys, nil
}

/*
tokenizerDone flushes the tokenizer stream and returns any remaining Morton keys
in its buffer. Called at the end of tokenizeStream before returning.
*/
func (machine *Machine) tokenizerDone() ([]uint64, error) {
	tokClient := errnie.Guard(machine.state, func() (tokenizer.Universal, error) {
		raw, err := machine.booter.router.Get(machine.ctx, cluster.TOKENIZER, "machine")
		return tokenizer.Universal(raw), err
	})

	future, release := tokClient.Done(machine.ctx, nil)
	defer release()

	results := errnie.Guard(machine.state, func() (tokenizer.Universal_done_Results, error) {
		return future.Struct()
	})

	keyList := errnie.Guard(machine.state, func() (capnp.UInt64List, error) {
		return results.Keys()
	})

	keys := make([]uint64, keyList.Len())

	for index := 0; index < keyList.Len(); index++ {
		keys[index] = keyList.At(index)
	}

	return keys, nil
}

/*
writeKeys passes the tokenizer key stream into both spatialIndex and graph.
*/
func (machine *Machine) writeKeys(ctx context.Context, keys []uint64) {
	coder := data.NewMortonCoder()
	var previousSymbol byte
	havePrevious := false

	forest := errnie.Guard(machine.state, func() (server.Server, error) {
		raw, err := machine.booter.router.Get(ctx, cluster.FOREST, "machine")
		return server.Server(raw), err
	})

	graph := errnie.Guard(machine.state, func() (substrate.Graph, error) {
		raw, err := machine.booter.router.Get(ctx, cluster.GRAPH, "machine")
		return substrate.Graph(raw), err
	})

	for _, key := range keys {
		errnie.GuardVoid(machine.state, func() error {
			return forest.Write(
				ctx, func(p server.Server_write_Params) error {
					p.SetKey(key)
					return nil
				},
			)
		})

		errnie.GuardVoid(machine.state, func() error {
			return forest.WaitStreaming()
		})

		errnie.GuardVoid(machine.state, func() error {
			return graph.Write(
				ctx, func(p substrate.Graph_write_Params) error {
					p.SetKey(key)
					return nil
				},
			)
		})

		errnie.GuardVoid(machine.state, func() error {
			return graph.WaitStreaming()
		})

		_, symbol := coder.Unpack(key)
		if havePrevious {
			machine.sink.Emit(telemetry.Event{
				Component: "LSM",
				Action:    "Insert",
				Data: telemetry.EventData{
					Left:      int(previousSymbol),
					Right:     int(symbol),
					Edges:     1,
					EdgeCount: 1,
					ChunkText: fmt.Sprintf("%c→%c", previousSymbol, symbol),
				},
			})
		}

		previousSymbol = symbol
		havePrevious = true
	}
}

/*
emitTokenizerTelemetryFromKeys emits token-level geometry events.
*/
func (machine *Machine) emitTokenizerTelemetryFromKeys(keys []uint64, stage string) {
	coder := data.NewMortonCoder()
	symbols := make([]byte, len(keys))

	for index, key := range keys {
		_, symbol := coder.Unpack(key)
		symbols[index] = symbol
	}

	for index, key := range keys {
		position, symbol := coder.Unpack(key)
		value := primitive.BaseValue(symbol)
		rolled := value.RollLeft(int(position))
		chunkStart := max(index-4, 0)
		chunkEnd := min(index+5, len(symbols))
		chunkText := string(symbols[chunkStart:chunkEnd])

		machine.sink.Emit(telemetry.Event{
			Component: "Tokenizer",
			Action:    "Value",
			Data: telemetry.EventData{
				ValueID:    int(position),
				Bin:        rolled.Bin(),
				State:      "stored",
				ActiveBits: primitive.ValuePrimeIndices(&rolled),
				Density:    rolled.ShannonDensity(),
				ChunkText:  chunkText,
				Stage:      stage,
				EdgeCount:  1,
			},
		})
	}
}

func compilePromptSequence(keys []uint64) ([]primitive.Value, []primitive.Value) {
	state := errnie.NewState("vm/compilePromptSequence")
	cells := primitive.CompileSequenceCells(keys)
	values := make([]primitive.Value, 0, len(cells))
	metaValues := make([]primitive.Value, 0, len(cells))

	for _, cell := range cells {
		values = append(values, primitive.SeedObservable(cell.Symbol, cell.Value))

		meta := errnie.Guard(state, func() (primitive.Value, error) {
			return primitive.New()
		})
		meta.CopyFrom(cell.Meta)
		metaValues = append(metaValues, meta)
	}

	return values, metaValues
}

func compilePromptRows(keys []uint64) [][]primitive.Value {
	values := primitive.CompileObservableSequenceValues(keys)

	if len(values) == 0 {
		return nil
	}

	rows := make([][]primitive.Value, 0, 8)
	rowStart := 0

	for index, key := range keys {
		position, _ := coder.Unpack(key)

		if index == 0 || position != 0 {
			continue
		}

		rows = append(rows, append([]primitive.Value(nil), values[rowStart:index]...))
		rowStart = index
	}

	rows = append(rows, append([]primitive.Value(nil), values[rowStart:]...))

	return rows
}

func valueMatrixToPointerList(
	messageOption capnp.Arena,
	values [][]primitive.Value,
) (capnp.PointerList, error) {
	_, seg, err := capnp.NewMessage(messageOption)
	if err != nil {
		return capnp.PointerList{}, err
	}

	state := errnie.NewState("vm/valueMatrix")

	list := errnie.Guard(state, func() (capnp.PointerList, error) {
		return capnp.NewPointerList(seg, int32(len(values)))
	})

	for index, row := range values {
		valueList := errnie.Guard(state, func() (primitive.Value_List, error) {
			return valueListFromSlice(seg, row)
		})

		errnie.GuardVoid(state, func() error {
			return list.Set(index, valueList.ToPtr())
		})
	}

	if state.Failed() {
		return capnp.PointerList{}, state.Err()
	}

	return list, nil
}

func decodeResultPaths(resultPaths capnp.PointerList, skip int) ([]byte, error) {
	if resultPaths.Len() == 0 {
		return nil, nil
	}

	state := errnie.NewState("vm/decodeResultPaths")

	ptr := errnie.Guard(state, func() (capnp.Ptr, error) {
		return resultPaths.At(0)
	})

	values := errnie.Guard(state, func() ([]primitive.Value, error) {
		return primitive.ValueListToSlice(primitive.Value_List(ptr.List()))
	})

	if state.Failed() {
		return nil, state.Err()
	}

	fmt.Printf("DEBUG: decodeResultPaths values=%d, skip=%d\n", len(values), skip)

	if skip >= len(values) {
		return []byte{}, nil
	}

	return decodeObservableValues(values[skip:]), nil
}

func decodeObservableValues(values []primitive.Value) []byte {
	result := make([]byte, 0, len(values))

	for _, value := range values {
		symbol, ok := primitive.InferLexicalSeed(value)
		if !ok {
			continue
		}

		result = append(result, symbol)
	}

	return result
}

func valueListFromSlice(
	seg *capnp.Segment,
	values []primitive.Value,
) (primitive.Value_List, error) {
	state := errnie.NewState("vm/valueListFromSlice")

	valueList := errnie.Guard(state, func() (primitive.Value_List, error) {
		return primitive.NewValue_List(seg, int32(len(values)))
	})

	for index, value := range values {
		dst := valueList.At(index)
		dst.CopyFrom(value)
	}

	if state.Failed() {
		return primitive.Value_List{}, state.Err()
	}

	return valueList, nil
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
	ErrMachineMissingRequirements MachineError = "missing requirements"
)

func (machineError MachineError) Error() string {
	return string(machineError)
}
