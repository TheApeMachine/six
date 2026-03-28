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

All machineOption functions (WithContext, WithDataset, WithRegionsCount, etc.) apply only
during NewMachine construction. Options mutate the in-progress *Machine before the pool,
regions, and transport stream are wired up; calling them after NewMachine returns is
unsupported and may race with in-flight I/O or scheduling.
*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	pool         *Pool
	stream       *transport.Stream
	regions      []*primitive.Region
	dataset      io.ReadCloser
	regionsCount int
	// writePending holds a tail shorter than primitive.ByteSize between Writes.
	// io.Copy uses arbitrary buffer sizes; transport.Stream requires each Write
	// to be a multiple of ByteSize.
	writePending []byte
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

	if machine.pool, machine.err = NewPool(
		PoolWithContext(machine.ctx),
		PoolWithProcs(maxProcs),
	); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", machine.err,
		)
	}
	// Workers must run before Schedule jobs are processed.
	machine.pool.Run()

	regionsCount := machine.regionsCount
	if regionsCount <= 0 {
		regionsCount = runtime.NumCPU()
		if regionsCount < 1 {
			regionsCount = 1
		}
	}

	machine.regions = make([]*primitive.Region, 0, regionsCount)

	for i := 0; i < regionsCount; i++ {
		machine.regions = append(machine.regions, primitive.NewRegion(uint64(i)))
	}

	var streamErr error
	machine.stream, streamErr = transport.NewStream(
		transport.WithContext(machine.ctx),
		transport.WithRegions(machine.regions),
	)
	if streamErr != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"error", streamErr,
		)
	}

	return machine, nil
}

func (machine *Machine) Pool() *Pool {
	return machine.pool
}

// Regions returns the machine's Region set. Callers can read accumulated
// frames directly from Regions after the recirculation loop has stopped.
func (machine *Machine) Regions() []*primitive.Region {
	return machine.regions
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.stream.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if machine.stream == nil {
		return 0, errnie.Error(
			NewMachineError(MachineErrFailStart),
			"msg", "nil stream",
		)
	}

	machine.writePending = append(machine.writePending, p...)
	aligned := len(machine.writePending) - (len(machine.writePending) % primitive.ByteSize)
	if aligned == 0 {
		return len(p), nil
	}

	if _, err = machine.stream.Write(machine.writePending[:aligned]); err != nil {
		return 0, errnie.Error(err)
	}
	machine.writePending = append(machine.writePending[:0], machine.writePending[aligned:]...)
	return len(p), nil
}

// flushWritePending pads any tail to a full frame with zeros and forwards it.
// Call before CloseWrite / stream teardown so the byte length is a multiple of ByteSize.
func (machine *Machine) flushWritePending() error {
	if machine.stream == nil || len(machine.writePending) == 0 {
		return nil
	}
	pending := machine.writePending
	rem := len(pending) % primitive.ByteSize
	var block []byte
	if rem == 0 {
		block = pending
	} else {
		block = make([]byte, len(pending)+primitive.ByteSize-rem)
		copy(block, pending)
	}
	if _, err := machine.stream.Write(block); err != nil {
		return err
	}
	machine.writePending = machine.writePending[:0]
	return nil
}

// CloseWrite signals EOF to stream readers after the last framed Write.
func (machine *Machine) CloseWrite() error {
	if machine.stream == nil {
		return nil
	}
	if err := machine.flushWritePending(); err != nil {
		return errnie.Error(err)
	}
	return machine.stream.CloseWrite()
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
		if err := machine.flushWritePending(); err != nil {
			errs = append(errs, err)
		}
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

// WithRegionsCount sets how many primitive.Region instances are created. When unset or
// non-positive, the count defaults to runtime.NumCPU() (minimum 1).
func WithRegionsCount(n int) machineOption {
	return func(machine *Machine) {
		if n > 0 {
			machine.regionsCount = n
		}
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
