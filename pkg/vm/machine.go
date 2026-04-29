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
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	host      *network.Host
	tokenizer *Tokenizer
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	firmware  *compute.Firmware
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

	telemetryURL := ""
	if core.Cfg.TelemetryEnabled {
		telemetryURL = core.Cfg.TelemetryWebSocketURL
	}

	bridge, err := telemetry.NewBridge(ctx, telemetryURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		telemetry: bridge,
		backend:   compute.NewBackend(ctx),
		firmware:  compute.NewFirmware(ctx),
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
		"firmware":  machine.firmware,
	})
}

/*
Close the machine.
*/
func (machine *Machine) Close() (err error) {
	if machine == nil {
		return nil
	}

	if machine.cancel != nil {
		machine.cancel()
	}

	if machine.telemetry != nil {
		if e := machine.telemetry.Close(); e != nil {
			err = errors.Join(err, errnie.Error(e))
		}
	}

	if machine.host != nil {
		if e := machine.host.Close(); e != nil {
			err = errors.Join(err, errnie.Error(e))
		}
	}

	if machine.tokenizer != nil {
		if e := machine.tokenizer.Close(); e != nil {
			err = errors.Join(err, errnie.Error(e))
		}
	}

	if machine.backend != nil {
		if e := machine.backend.Close(); e != nil {
			err = errors.Join(err, errnie.Error(e))
		}
	}

	return err
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
Cycle runs one backend sweep. READY Values execute against their SELECTED
reference lane when the query program tagged one; otherwise they see the full
resident community.
*/
func (machine *Machine) Cycle() (
	[]*primitive.Value,
	[]*primitive.Value,
	error,
) {
	if machine.backend == nil {
		return nil, nil, errors.New("backend is nil")
	}

	resolved := make([]*primitive.Value, 0)
	ready := make([]*primitive.Value, 0)

	for result := range machine.backend.Sync(machine.ctx) {
		resolved = append(resolved, result.Resolved...)
		ready = append(ready, result.Ready...)
	}

	return resolved, ready, nil
}

/*
Range visits the backend resident store through Machine so integration tests
can inspect in-band state without reaching into backend ownership.
*/
func (machine *Machine) Range(visitor func(*primitive.Value) bool) {
	if machine == nil || machine.backend == nil {
		return
	}

	machine.backend.Range(visitor)
}

/*
Load ingests the dataset and seeds the bootstrap query/recruiter pair. The
query carries a key/value selector in context and wakes the recruiter after
tagging matching residents in-band.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errors.Join(machine.err, errnie.Error(err))
	}

	if machine.telemetry != nil {
		if err := machine.telemetry.BeginRun(); err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}
	}

	for sample := range dataset.Generate() {
		segments, err := machine.tokenizer.IngestSample(
			machine.ctx, sample,
		)

		if err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		for _, segment := range segments {
			machine.backend.Submit(segment)

			if machine.telemetry != nil {
				_, err := io.Copy(machine.telemetry, segment)

				if err != nil {
					return errnie.Error(err)
				}
			}
		}
	}

	deployed, err := machine.firmware.Deploy(
		core.RECRUIT_COMMUNITY, []uint64{
			uint64(primitive.PropertyWord(primitive.COMMUNITY)), 0,
		},
		nil,
	)
	if err != nil {
		return err
	}

	for _, value := range deployed {
		machine.backend.Submit(value)

		if machine.telemetry != nil {
			_, err := io.Copy(machine.telemetry, value)

			if err != nil {
				return errnie.Error(err)
			}
		}
	}

	for {
		_, ready, err := machine.Cycle()
		if err != nil {
			return err
		}

		if machine.telemetry != nil {
			var telemetryErr error

			machine.backend.Range(func(value *primitive.Value) bool {
				if telemetryErr != nil {
					return false
				}

				frame := value.Bytes()
				if len(frame) == 0 {
					return true
				}

				_, telemetryErr = machine.telemetry.Write(frame)

				return telemetryErr == nil
			})

			if telemetryErr != nil {
				return errnie.Error(telemetryErr)
			}
		}

		if len(ready) == 0 {
			break
		}
	}

	return nil
}

/*
Prompt injects prompt segment Values into the community, cycles until settled,
and returns Values that newly settled in the RESOLVED state. Linked prompt
tails still retire DONE in resident state and are observable through the
backend store.
*/
func (machine *Machine) Prompt(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errors.Join(machine.err, errnie.Error(err))
	}

	if len(values) == 0 {
		return nil, errors.New("prompt requires at least one value")
	}

	resolved = make([]*primitive.Value, 0)
	promptIDs := make(map[uint64]struct{}, len(values))

	for _, value := range values {
		if value == nil {
			return nil, errors.New("prompt value is nil")
		}
	}

	promptHeadID := values[0].ID()

	for _, value := range values {
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		value.SetProperty(primitive.REFERENCE, promptHeadID)
		promptIDs[value.ID()] = struct{}{}

		if err := machine.backend.Submit(value); err != nil {
			return nil, errnie.Error(err)
		}

		if machine.telemetry != nil {
			if _, err := io.Copy(machine.telemetry, value); err != nil {
				return nil, errnie.Error(err)
			}
		}
	}

	const maxPromptSettleCycles = 1_000_000
	settleIterations := 0

	for len(resolved) == 0 {
		select {
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()
		default:
		}

		settleIterations++
		if settleIterations > maxPromptSettleCycles {
			return nil, errors.New("prompt settle exceeded maximum cycles")
		}

		var ready []*primitive.Value
		var candidates []*primitive.Value

		if candidates, ready, err = machine.Cycle(); err != nil {
			return nil, err
		}

		for _, value := range candidates {
			if machine.telemetry != nil {
				if _, err := io.Copy(machine.telemetry, value); err != nil {
					return nil, errnie.Error(err)
				}
			}

			if _, ok := promptIDs[value.ID()]; ok && value.Role() == primitive.ValueRolePrompt {
				resolved = append(resolved, value)
			}
		}

		if len(resolved) == 0 && len(ready) == 0 {
			return nil, errors.New("prompt settled without resolved values")
		}
	}

	return resolved, nil
}
