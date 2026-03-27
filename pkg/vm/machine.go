package vm

import (
	"context"
	"errors"
	"io"
	"runtime"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Machine provides a unified stream processing pipeline using github.com/whitaker-io/machine
and ants goroutine pooling. It dynamically schedules work across local hardware substrates
and remote network nodes natively through a homogeneous data flow, while guaranteeing
zero-drop fault tolerance using errnie error handling contexts.
*/
type Machine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	pool    *Pool
	stream  *transport.Stream
	regions []*primitive.Region
	dataset io.ReadCloser
}

type machineOption func(*Machine)

func NewMachine(opts ...machineOption) (machine *Machine, err error) {
	maxProcs := runtime.NumCPU() - 1

	errnie.Info(
		"vm.machine.NewMachine",
		"poolProcs", maxProcs,
	)

	machine = &Machine{
		stream: transport.NewStream(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
		"stream": machine.stream,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", machine.err,
		)
	}

	if machine.pool, machine.err = NewPool(
		PoolWithContext(machine.ctx),
		PoolWithProcs(maxProcs),
	); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", machine.err,
		)
	}

	machine.regions = make([]*primitive.Region, 0)
	var region *primitive.Region

	for idx := range 10 {
		if region, machine.err = primitive.NewRegion(
			uint64(idx),
		); machine.err != nil {
			return nil, errnie.Error(
				NewMachineError(MachineErrFailStart),
				"error", machine.err,
			)
		}

		machine.regions = append(machine.regions, region)
	}

	machine.stream = transport.NewStream(
		transport.WithContext(machine.ctx),
		transport.WithRegions(machine.regions),
	)

	return machine, nil
}

func (machine *Machine) Pool() *Pool {
	return machine.pool
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.stream.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	return machine.stream.Write(p)
}

func (machine *Machine) Close() error {
	machine.cancel()
	return nil
}

func WithContext(ctx context.Context) machineOption {
	return func(machine *Machine) {
		machine.ctx, machine.cancel = context.WithCancel(ctx)
		errnie.Info(
			"vm.machine.WithContext",
			"msg",
			"context set",
		)
	}
}

func WithDataset(dataset io.ReadCloser) machineOption {
	return func(machine *Machine) {
		machine.dataset = dataset
	}
}

type MachineErrorType string

const (
	MachineErrFailStart MachineErrorType = "failed to start machine"
)

type MachineError struct {
	Err error
	Msg string
}

func (err *MachineError) Error() string {
	return err.Msg
}

func NewMachineError(err MachineErrorType) *MachineError {
	return &MachineError{
		Msg: string(err),
		Err: errors.New(string(err)),
	}
}
