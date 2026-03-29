package vm

import (
	"context"
	"errors"
	"fmt"
	"io"

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
Machine provides a unified stream processing pipeline. All machine options are
applied during construction so tests and callers can instantiate isolated
machines without relying on package-level state.
*/
type Machine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	stream  *transport.Stream
	dataset io.ReadCloser
	config  SubstrateConfig

	regions     int
	autoS3      bool
	streamOpts  []transport.StreamOption
	streamSides []io.ReadWriter
}

type machineOption func(*Machine)

func WithConfig(config SubstrateConfig) machineOption {
	return func(machine *Machine) {
		machine.config = config
	}
}

// WithRegionsCount configures how many parallel stream regions are constructed.
func WithRegionsCount(n int) machineOption {
	return func(machine *Machine) {
		if n > 0 {
			machine.regions = n
		}
	}
}

// WithStreamOptions appends low-level transport options to the machine.
func WithStreamOptions(opts ...transport.StreamOption) machineOption {
	return func(machine *Machine) {
		machine.streamOpts = append(machine.streamOpts, opts...)
	}
}

// WithStreamAdapter attaches an additional observer / sink to the stream.
func WithStreamAdapter(side io.ReadWriter) machineOption {
	return func(machine *Machine) {
		if side != nil {
			machine.streamSides = append(machine.streamSides, side)
		}
	}
}

// WithS3Adapter opts in to the LakeFS / MinIO adapter. It is disabled by
// default so NewMachine remains side-effect free in tests.
func WithS3Adapter() machineOption {
	return func(machine *Machine) {
		machine.autoS3 = true
	}
}

// NewMachine constructs a Machine: it applies opts, validates ctx/cancel, and
// wires an isolated transport stream.
func NewMachine(ctx context.Context, opts ...machineOption) (machine *Machine, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	machine = &Machine{regions: 1}
	machine.ctx, machine.cancel = context.WithCancel(ctx)

	for _, opt := range opts {
		opt(machine)
	}

	if machine.config.ValueConfig == nil {
		machine.config.ValueConfig = core.Cfg
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(MachineErrFailStart, machine.err),
		)
	}

	streamOpts := make([]transport.StreamOption, 0, len(machine.streamOpts)+1+len(machine.streamSides)+1)
	streamOpts = append(streamOpts, transport.StreamWithRegions(machine.regions))
	streamOpts = append(streamOpts, machine.streamOpts...)

	if machine.autoS3 {
		s3adapter, s3Err := adapter.NewS3Adapter(machine.ctx)
		if s3Err == nil && s3adapter != nil {
			streamOpts = append(streamOpts, transport.StreamWithAdapter(s3adapter))
		} else if s3Err != nil {
			errnie.Trace("vm.NewMachine", "s3_adapter", "disabled/not_configured: "+s3Err.Error())
		}
	}

	for _, side := range machine.streamSides {
		streamOpts = append(streamOpts, transport.StreamWithAdapter(side))
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
