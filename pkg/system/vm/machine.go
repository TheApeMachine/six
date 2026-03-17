package vm

import (
	"context"
	"fmt"
	"runtime"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/dmt/server"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/system/vm/input"
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

	promptFuture, promptRelease := machine.booter.prompter.Generate(
		ctx, func(p input.Prompter_generate_Params) error {
			return p.SetMsg(msg)
		},
	)
	defer promptRelease()

	promptResult := errnie.Guard(machine.state, func() (input.Prompter_generate_Results, error) {
		return promptFuture.Struct()
	})

	promptBytes := errnie.Guard(machine.state, func() ([]byte, error) {
		return promptResult.Data()
	})

	keys := errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizeStream(promptBytes)
	})

	if machine.state.Failed() {
		return nil, machine.state.Err()
	}

	promptValues, _ := compilePromptSequence(keys)

	if len(promptValues) == 0 {
		return nil, nil
	}

	paths := errnie.Guard(machine.state, func() (capnp.PointerList, error) {
		return valueMatrixToPointerList(
			capnp.SingleSegment(nil),
			[][]data.Value{promptValues},
		)
	})

	graphFuture, graphRelease := machine.booter.graph.Prompt(ctx, func(
		p substrate.Graph_prompt_Params,
	) error {
		return p.SetPaths(paths)
	})

	defer graphRelease()

	graphResult := errnie.Guard(machine.state, func() (substrate.Graph_prompt_Results, error) {
		return graphFuture.Struct()
	})

	resultPaths := errnie.Guard(machine.state, func() (capnp.PointerList, error) {
		return graphResult.Result()
	})

	result := errnie.Guard(machine.state, func() ([]byte, error) {
		return decodeResultPaths(resultPaths, len(promptValues))
	})

	if machine.state.Failed() {
		return nil, machine.state.Err()
	}

	fmt.Printf("DEBUG: msg=%q, promptBytes=%q, keys=%d, promptValues=%d, resultPathsLen=%d, result=%q\n",
		msg, string(promptBytes), len(keys), len(promptValues), resultPaths.Len(), string(result))

	stage := "prompt-empty"

	if len(result) > 0 {
		stage = "prompt-complete"
	}

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:      stage,
			Message:    msg,
			ResultText: string(result),
		},
	})

	return result, nil
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

	for tok := range dataset.Generate() {
		errnie.GuardVoid(machine.state, func() error {
			return machine.booter.tok.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(tok.Symbol)
					return nil
				},
			)
		})
	}

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

	machine.writeKeys(ctx, keys)

	errnie.GuardVoid(machine.state, func() error {
		future, release := machine.booter.graph.Done(ctx, nil)
		defer release()
		_, err := future.Struct()
		return err
	})

	return machine.state.Err()
}

/*
tokenizeStream feeds bytes through the tokenizer and returns the exact keys.
*/
func (machine *Machine) tokenizeStream(raw []byte) ([]uint64, error) {
	ctx := machine.ctx
	keys := make([]uint64, 0, len(raw))

	for _, symbol := range raw {
		errnie.GuardVoid(machine.state, func() error {
			return machine.booter.tok.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(symbol)
					return nil
				},
			)
		})
	}

	drained := errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	keys = append(keys, drained...)

	drained = errnie.Guard(machine.state, func() ([]uint64, error) {
		return machine.tokenizerDone()
	})

	keys = append(keys, drained...)

	return keys, nil
}

func (machine *Machine) tokenizerDone() ([]uint64, error) {
	future, release := machine.booter.tok.Done(machine.ctx, nil)
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
	for _, key := range keys {
		errnie.GuardVoid(machine.state, func() error {
			return machine.booter.forestClient.Write(
				ctx, func(p server.Server_write_Params) error {
					p.SetKey(key)
					return nil
				},
			)
		})

		errnie.GuardVoid(machine.state, func() error {
			return machine.booter.graph.Write(
				ctx, func(p substrate.Graph_write_Params) error {
					p.SetKey(key)
					return nil
				},
			)
		})
	}
}

func compilePromptSequence(keys []uint64) ([]data.Value, []data.Value) {
	cells := data.CompileSequenceCells(keys)
	values := make([]data.Value, 0, len(cells))
	metaValues := make([]data.Value, 0, len(cells))

	for _, cell := range cells {
		values = append(values, data.SeedObservable(cell.Symbol, cell.Value))

		meta := data.MustNewValue()
		meta.CopyFrom(cell.Meta)
		metaValues = append(metaValues, meta)
	}

	return values, metaValues
}

func valueMatrixToPointerList(
	messageOption capnp.Arena,
	values [][]data.Value,
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
		valueList := errnie.Guard(state, func() (data.Value_List, error) {
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

	values := errnie.Guard(state, func() ([]data.Value, error) {
		return data.ValueListToSlice(data.Value_List(ptr.List()))
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

func decodeObservableValues(values []data.Value) []byte {
	result := make([]byte, 0, len(values))

	for _, value := range values {
		symbol, ok := data.InferLexicalSeed(value)
		if !ok {
			continue
		}

		result = append(result, symbol)
	}

	return result
}

func valueListFromSlice(
	seg *capnp.Segment,
	values []data.Value,
) (data.Value_List, error) {
	state := errnie.NewState("vm/valueListFromSlice")

	valueList := errnie.Guard(state, func() (data.Value_List, error) {
		return data.NewValue_List(seg, int32(len(values)))
	})

	for index, value := range values {
		dst := valueList.At(index)
		dst.CopyFrom(value)
	}

	if state.Failed() {
		return data.Value_List{}, state.Err()
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
