package vm

import (
	"context"
	"errors"
	"io"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Machine is the central runtime that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx              context.Context
	cancel           context.CancelFunc
	err              error
	host             *network.Host
	tokenizer        *Tokenizer
	backend          *compute.Backend
	telemetry        *telemetry.Bridge
	telemetryCopyBuf []byte
	community        []*primitive.Value
	ready            []*primitive.Value
	resolved         []*primitive.Value
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	if core.Cfg == nil || len(core.Cfg.Programs) == 0 {
		_ = core.LoadDefaultConfig()
		core.NewConfig()
	}

	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:              ctx,
		cancel:           cancel,
		telemetry:        bridge,
		telemetryCopyBuf: make([]byte, 32*1024),
		backend:          compute.NewBackend(ctx),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		machine.Close()

		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
		machine.Close()
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"tokenizer": machine.tokenizer,
		"backend":   machine.backend,
	})
}

/*
Close the machine.
*/
func (machine *Machine) Close() error {
	var errs []error

	if machine == nil {
		return nil
	}

	if machine.cancel != nil {
		machine.cancel()
	}

	if len(machine.community) > 0 {
		primitive.CloseAll(machine.community)
		clear(machine.community)
		machine.community = nil
	}

	if machine.telemetry != nil {
		if err := machine.telemetry.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.host != nil {
		if err := machine.host.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.tokenizer != nil {
		if err := machine.tokenizer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.backend != nil {
		if err := machine.backend.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

/*
Error implements the error interface.
*/
func (machine *Machine) Error() string {
	if machine == nil || machine.err == nil {
		return ""
	}

	return machine.err.Error()
}

/*
Cycle executes one observed community tick through the backend.
Callers repeat it until continuations settle or a resolved Value emerges.
*/
func (machine *Machine) Cycle() (resolved []*primitive.Value, err error) {
	select {
	case <-machine.ctx.Done():
		return nil, machine.ctx.Err()
	default:
	}

	if machine.backend == nil || len(machine.community) == 0 {
		return nil, nil
	}

	if len(machine.ready) > 1 {
		if err := machine.backend.Submit(machine.ready[0], machine.ready[1:]); err != nil {
			return nil, errnie.Error(err)
		}
	}

	for spawned := range machine.backend.Sync(machine.ctx) {
		if spawned == nil {
			continue
		}

		if machine.telemetry != nil {
			_, _ = io.CopyBuffer(machine.telemetry, spawned, machine.telemetryCopyBuf)
		}

		if spawned.Status() != primitive.READY {
			machine.ready = append(machine.ready, spawned)
			continue
		}

		machine.community = append(machine.community, spawned)
	}

	return machine.resolved, nil
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then cycles recruitment until the
community word stops changing.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	var segments []*primitive.Value

	// Spark the community forming by emitting a recruiter Value.
	machine.ready = append(machine.ready, primitive.Emit(
		primitive.WithFirmware(core.RECRUIT_COMMUNITY),
		primitive.WithStatus(uint64(primitive.READY)),
	))

	for sample := range dataset.Generate() {
		if segments, err = machine.tokenizer.IngestSample(
			machine.ctx, sample,
		); err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		machine.community = append(machine.community, segments...)
		machine.ready = append(machine.ready, segments...)

		for _, value := range machine.ready {
			value.SetStatus(primitive.READY)
			if machine.telemetry != nil {
				_, _ = io.CopyBuffer(machine.telemetry, value, machine.telemetryCopyBuf)
			}
		}

		if _, err := machine.Cycle(); err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		readers := make([]io.Reader, len(machine.community))

		for i, value := range machine.community {
			readers[i] = value
		}

		if machine.telemetry != nil {
			_, _ = io.CopyBuffer(
				machine.telemetry, io.MultiReader(readers...), machine.telemetryCopyBuf,
			)
		}
	}

	if _, err := machine.Cycle(); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	return nil
}

/*
Prompt injects the prompt segment Values into the community and cycles until settled.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (
	resolved []*primitive.Value, err error,
) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errors.Join(machine.err, errnie.Error(err))
	}

	machine.community = append(machine.community, values...)

	done := false

	for !done {
		if resolved, err = machine.Cycle(); err != nil {
			return nil, errors.Join(machine.err, errnie.Error(err))
		}

		if len(resolved) > 0 {
			done = true
		}
	}

	return resolved, nil
}
