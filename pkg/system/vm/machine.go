package vm

import (
	"context"
	"runtime"
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
Prompt is the main entry point.
*/
func (machine *Machine) Prompt(msg string) ([]byte, error) {
	ctx := machine.ctx

	machine.sink.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage: "prompt-start", Msg: msg,
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

	for _, b := range promptBytes {
		if err := machine.booter.tok.Write(ctx, func(p tokenizer.Universal_write_Params) error {
			p.SetData(b)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if err := machine.tokenizerDone(); err != nil {
		return nil, err
	}

	if err := machine.tokenizerDone(); err != nil {
		return nil, err
	}

	return []byte{}, nil
}

/*
SetDataset streams the dataset directly through the tokenizer into the spatial
index. No buffering, no string reassembly.
*/
func (machine *Machine) SetDataset(dataset provider.Dataset) error {
	ctx := machine.ctx
	started := false

	for tok := range dataset.Generate() {
		if started && tok.Pos == 0 {
			if err := machine.tokenizerDone(); err != nil {
				return err
			}
		}

		if err := machine.booter.tok.Write(
			ctx, func(p tokenizer.Universal_write_Params) error {
				p.SetData(tok.Symbol)
				return nil
			},
		); err != nil {
			return err
		}

		started = true
	}

	if started {
		if err := machine.tokenizerDone(); err != nil {
			return err
		}
	}

	if err := machine.tokenizerDone(); err != nil {
		return err
	}

	errnie.SafeMustVoid(func() error {
		return machine.booter.spatialIndex.WaitStreaming()
	})

	return nil
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

	if err := machine.tokenizerDone(); err != nil {
		return nil
	}

	if err := machine.tokenizerDone(); err != nil {
		return nil
	}

	return keys
}

func (machine *Machine) tokenizerDone() error {
	future, release := machine.booter.tok.Done(machine.ctx, nil)
	defer release()

	_, err := future.Struct()

	return err
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
