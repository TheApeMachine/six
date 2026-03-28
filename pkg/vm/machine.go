package vm

import (
	"context"
	"errors"
	"io"
	"runtime"

	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Machine provides a unified stream processing pipeline using github.com/whitaker-io/machine
and ants goroutine pooling. It dynamically schedules work across local hardware substrates
and remote network nodes natively through a homogeneous data flow, while guaranteeing
zero-drop fault tolerance using errnie error handling contexts.

All machineOption functions (WithContext, WithDataset, WithRegionsCount, etc.) apply only
during NewMachine construction. Options mutate the in-progress *Machine before the pool,
regions, and transport stream are wired up; calling them after NewMachine returns is
unsupported and may race with in-flight I/O or scheduling.
*/
type Machine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	stream  *transport.Stream
	dataset io.ReadCloser
}

type machineOption func(*Machine)

// NewMachine constructs a Machine: it applies opts, validates ctx/cancel, starts the pool
// workers, allocates regions (see WithRegionsCount), and opens the transport stream.
// Pass all options only via opts during this call—do not reuse machineOption implementations
// on an already-built *Machine (see WithContext).
func NewMachine(opts ...machineOption) (machine *Machine, err error) {
	cpu := runtime.NumCPU()
	maxProcs := cpu - 1

	if cpu <= 1 {
		maxProcs = 1
	}

	errnie.Info(
		"vm.machine.NewMachine",
		"poolProcs", maxProcs,
	)

	ctx, cancel := context.WithCancel(context.Background())

	machine = &Machine{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", machine.err,
		)
	}

	machine.stream = transport.NewStream(
		transport.StreamWithContext(machine.ctx),
	)

	if machine.stream == nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", "stream failed to start",
		)
	}

	return machine, nil
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.stream.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	return machine.stream.Write(p)
}

func (machine *Machine) Close() error {
	if machine.cancel != nil {
		machine.cancel()
	}

	var errs []error

	if machine.dataset != nil {
		if err := machine.dataset.Close(); err != nil {
			errs = append(errs, err)
		}
		machine.dataset = nil
	}

	if machine.stream != nil {
		if err := machine.stream.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// WithContext replaces the Machine's context and cancel func. It cancels Machine.cancel
// when one already exists. Intended for use only as a machineOption inside NewMachine:
// calling WithContext after the Machine is returned can race with concurrent Read/Write,
// pool workers, and stream I/O because the previous context is canceled immediately.
func WithContext(ctx context.Context) machineOption {
	return func(machine *Machine) {
		if machine.cancel != nil {
			machine.cancel()
		}
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

func (err *MachineError) Unwrap() error {
	return err.Err
}

func NewMachineError(err MachineErrorType) *MachineError {
	return &MachineError{
		Msg: string(err),
		Err: errors.New(string(err)),
	}
}
