package vm

import (
	"context"
	"errors"

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
Machine is a central orchestrator that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	host      *network.Host
	tokenizer *Tokenizer
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	community []*primitive.Value
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		telemetry: bridge,
		backend:   compute.NewBackend(ctx),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
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

	machine.cancel()

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
Error returns the error of the machine.
*/
func (machine *Machine) Error() error {
	return machine.err
}

/*
Cycle executes the entire community loop until it converges (all continuations are 0)
or at least one newly resolved Value emerges.
*/
func (machine *Machine) Cycle() (resolved []*primitive.Value, err error) {
	select {
	case <-machine.ctx.Done():
		return nil, machine.ctx.Err()
	default:
		if machine.backend != nil {
			machine.community = append(machine.community, machine.backend.DrainSpawned()...)
			machine.backend.Submit(machine.community)
		}

		var newlyResolved []*primitive.Value
		for _, value := range machine.community {
			status, _ := value.Property(primitive.STATUS)
			if status == uint64(primitive.RESOLVED) {
				newlyResolved = append(newlyResolved, value)
			}

			if machine.telemetry != nil {
				machine.telemetry.Write(value.Bytes())
			}
		}

		return newlyResolved, nil
	}
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then runs Cycle.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	var segments []*primitive.Value

	for sample := range dataset.Generate() {
		if segments, err = machine.tokenizer.IngestSample(
			machine.ctx, sample,
		); err != nil {
			return errnie.Error(err)
		}

		machine.community = append(machine.community, segments...)

		if _, err := machine.Cycle(); err != nil {
			return errnie.Error(err)
		}

		if machine.backend != nil {
			machine.community = append(machine.community, machine.backend.Sync(machine.ctx)...)
		}
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
		return nil, errnie.Error(err)
	}

	targetIDs := make(map[uint64]bool)

	for _, value := range values {
		if value == nil {
			continue
		}
		targetIDs[value.ID()] = true
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		value.InstallFirmware(core.CLASSIFY_READOUT)
		machine.community = append(machine.community, value)
	}

	for range 100 {
		newlyResolved, err := machine.Cycle()
		if err != nil {
			return nil, err
		}

		if machine.backend != nil {
			machine.community = append(machine.community, machine.backend.Sync(machine.ctx)...)
			for _, value := range machine.community {
				status, _ := value.Property(primitive.STATUS)
				if status == uint64(primitive.RESOLVED) {
					newlyResolved = append(newlyResolved, value)
				}
			}
		}

		var matched []*primitive.Value
		for _, v := range newlyResolved {
			if targetIDs[v.ID()] {
				matched = append(matched, v)
				delete(targetIDs, v.ID())
			}
		}

		if len(matched) == 0 {
			resolved = append(resolved, newlyResolved...)
		} else {
			resolved = append(resolved, matched...)
		}

		if len(targetIDs) == 0 {
			break
		}
	}

	return resolved, nil
}
