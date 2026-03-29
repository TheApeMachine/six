package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"

	"go.uber.org/zap"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/transport"
	"github.com/theapemachine/six/pkg/transport/adapter"
)

type SubstrateConfig struct {
	ValueConfig *core.Config
	Logger      *zap.Logger
}

/*
Machine provides a unified stream processing pipeline using github.com/whitaker-io/machine
and ants goroutine pooling. It dynamically schedules work across local hardware substrates
and remote network nodes natively through a homogeneous data flow, while guaranteeing
zero-drop fault tolerance using errnie error handling contexts.

All machineOption functions (WithDataset, WithRegionsCount, etc.) apply only
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
	config  SubstrateConfig
}

type machineOption func(*Machine)

func WithConfig(config SubstrateConfig) machineOption {
	return func(machine *Machine) {
		machine.config = config
	}
}

// NewMachine constructs a Machine: it applies opts, validates ctx/cancel, starts the pool
// workers, allocates regions (see WithRegionsCount), and opens the transport stream.
func NewMachine(ctx context.Context, opts ...machineOption) (machine *Machine, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cpu := runtime.NumCPU()
	maxProcs := cpu - 1

	if cpu <= 1 {
		maxProcs = 1
	}

	errnie.Info(
		"vm.machine.NewMachine",
		"poolProcs", maxProcs,
	)

	machine = &Machine{}
	machine.ctx, machine.cancel = context.WithCancel(ctx)

	var streamOpts []transport.StreamOption

	for _, opt := range opts {
		opt(machine)
	}

	// Default to at least 1 region if none was set via streamOpts in opts.
	// We'll just append it directly for now, or ensure StreamWithRegions is called.
	streamOpts = append(streamOpts, transport.StreamWithRegions(1))

	if machine.err = validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart, machine.err),
		)
	}

	// Add the S3 Adapter to log stream to LakeFS/MinIO natively if enabled/configured
	s3adapter, err := adapter.NewS3Adapter(machine.ctx)

	if err == nil && s3adapter != nil {
		streamOpts = append(
			streamOpts,
			transport.StreamWithAdapter(s3adapter),
		)
	} else if err != nil {
		errnie.Trace("vm.NewMachine", "s3_adapter", "disabled/not_configured: "+err.Error())
	}

	machine.stream = transport.NewStream(machine.ctx, streamOpts...)

	if machine.stream == nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart, errors.New("stream failed to start")),
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

func NewMachineError(typ MachineErrorType, cause error) *MachineError {
	msg := string(typ)
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", typ, cause)
	}
	return &MachineError{
		Msg: msg,
		Err: cause,
	}
}
