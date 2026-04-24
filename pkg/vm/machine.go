package vm

import (
	"context"
	"errors"
	"log"

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

	go func() {
		_ = bridge.Connect()
	}()

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
	for {
		select {
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()
		default:
			// 1. Scheduler: Build the active queue based on Continuation
			active := make([]*primitive.Value, 0, len(machine.community))
			for _, value := range machine.community {
				cont := value.SchedulingNext()
				
				// Handle autonomous reprogramming via CONTINUATION
				if cont > 0 && cont <= 20 {
					var fw core.FirmwareType
					switch cont {
					case 1:
						fw = core.FOLD_SUBSTRATE
					case 2:
						fw = core.CAUSAL_EXPLORE
					case 3:
						fw = core.VOTE_SWARM
					}

					if fw != "" {
						value.InstallFirmware(fw)
						value.SetProperty(primitive.STATUS, 0)
						value.SetProperty(primitive.CONTINUATION, value.ID()) // Re-enter queue with new program
						active = append(active, value)
						continue
					}
				} else if cont != 0 {
					// We only implement "continuation = own id" for now.
					// For target ID routing, we would activate the target instead.
					active = append(active, value)
				}
			}

			if len(active) > 0 {
				spawned := machine.backend.ExecuteCommunity(active)
				if len(spawned) > 0 {
					machine.community = append(machine.community, spawned...)
				}
			}

			done := len(active) == 0
			var newlyResolved []*primitive.Value

			for _, value := range machine.community {
				status, _ := value.Property(primitive.STATUS)
				if status == uint64(primitive.RESOLVED) {
					newlyResolved = append(newlyResolved, value)
				}
				
				log.Printf("Value %d status: %d, continuation: %d, role: %d", value.ID(), status, value.SchedulingNext(), value.Role())

				if machine.telemetry != nil {
					machine.telemetry.Write(value.Bytes())
				}
			}

			if len(newlyResolved) > 0 {
				return newlyResolved, nil
			}

			if done {
				return nil, nil
			}
		}
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

	for _, value := range values {
		if value == nil {
			continue
		}
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		machine.community = append(machine.community, value)
	}

	return machine.Cycle()
}
