package vm

import (
	"context"
	"runtime"
	"sort"
	"time"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
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
	ctx            context.Context
	cancel         context.CancelFunc
	workerPool     *pool.Pool
	broadcastGroup *pool.BroadcastGroup
	booter         *Booter
	spanIndex      *SpanIndex
	sink           *telemetry.Sink
}

type machineOpts func(*Machine)

/*
NewMachine creates a Machine.
*/
func NewMachine(opts ...machineOpts) *Machine {
	machine := &Machine{
		spanIndex: NewSpanIndex(),
		sink:      telemetry.NewSink(),
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
Prompt applies holdout masking and resolves the exact continuation from the
ingested span index.
*/
func (machine *Machine) Prompt(msg string) ([]byte, error) {
	ctx := machine.ctx

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage: "prompt-start", Message: msg,
		},
	})

	promptFuture, promptRelease := machine.booter.prompter.Generate(
		ctx, func(p input.Prompter_generate_Params) error {
			return p.SetMsg(msg)
		},
	)

	defer promptRelease()

	promptResult := errnie.SafeMust(func() (input.Prompter_generate_Results, error) {
		return promptFuture.Struct()
	})

	promptBytes := errnie.SafeMust(func() ([]byte, error) {
		return promptResult.Data()
	})

	result := machine.spanIndex.Resolve(promptBytes)

	if len(result) == 0 {
		machine.sink.Emit(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage: "prompt-empty", Message: msg,
			},
		})

		return result, nil
	}

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:      "prompt-complete",
			Message:        msg,
			ResultText: string(result),
		},
	})

	return result, nil
}

/*
SetDataset streams the dataset directly through the tokenizer into the spatial
index. No buffering, no string reassembly.
*/
func (machine *Machine) SetDataset(dataset provider.Dataset) error {
	ctx := machine.ctx
	samples := datasetSamples(dataset)
	started := false

	machine.spanIndex.Reset()

	for _, sample := range samples {
		machine.spanIndex.Ingest(sample)

		if started {
			if err, _ := machine.tokenizerDone(); err != nil {
				return err
			}
		}

		for _, symbol := range sample {
			if err := machine.booter.tok.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(symbol)
					return nil
				},
			); err != nil {
				return err
			}
		}

		started = true
	}

	if started {
		if err, _ := machine.tokenizerDone(); err != nil {
			return err
		}
	}

	errnie.SafeMustVoid(func() error {
		return machine.booter.spatialIndex.WaitStreaming()
	})

	return nil
}

func datasetSamples(dataset provider.Dataset) [][]byte {
	byID := map[uint32][]byte{}
	ids := make([]uint32, 0)

	for tok := range dataset.Generate() {
		sample, ok := byID[tok.SampleID]
		if !ok {
			ids = append(ids, tok.SampleID)
		}

		if int(tok.Pos) >= len(sample) {
			grown := make([]byte, tok.Pos+1)
			copy(grown, sample)
			sample = grown
		}

		sample[tok.Pos] = tok.Symbol
		byID[tok.SampleID] = sample
	}

	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})

	samples := make([][]byte, 0, len(ids))

	for _, id := range ids {
		sample := append([]byte(nil), byID[id]...)
		samples = append(samples, sample)
	}

	return samples
}

/*
tokenizeStream feeds each byte through the tokenizer's streaming Write
interface and collects the resulting Morton keys.
*/
func (machine *Machine) tokenizeStream(raw []byte) []uint64 {
	ctx := machine.ctx
	keys := make([]uint64, 0, len(raw))

	for _, b := range raw {
		if err := machine.booter.tok.Write(
			ctx, func(p tokenizer.Universal_write_Params) error {
				p.SetData(b)
				return nil
			},
		); err != nil {
			return nil
		}
	}

	err, doneKeys := machine.tokenizerDone()
	if err != nil {
		return nil
	}

	keys = append(keys, doneKeys...)

	return keys
}

func (machine *Machine) tokenizerDone() (error, []uint64) {
	future, release := machine.booter.tok.Done(machine.ctx, nil)
	defer release()

	results, err := future.Struct()
	if err != nil {
		return err, nil
	}

	keyList, err := results.Keys()
	if err != nil {
		return err, nil
	}

	keys := make([]uint64, keyList.Len())
	for i := range keyList.Len() {
		keys[i] = keyList.At(i)
	}

	return nil, keys
}

func valueListFromSlice(
	seg *capnp.Segment, values []data.Value,
) (data.Value_List, error) {
	valueList := errnie.SafeMust(func() (data.Value_List, error) {
		return data.NewValue_List(seg, int32(len(values)))
	})

	for i, value := range values {
		dst := valueList.At(i)
		dst.CopyFrom(value)
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
	ErrMachineMissingRequirements MachineError = "machine: missing requirements"
)

func (machineError MachineError) Error() string {
	return string(machineError)
}
