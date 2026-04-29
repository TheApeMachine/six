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

	for _, closer := range []io.Closer{
		machine.telemetry,
		machine.host,
		machine.tokenizer,
		machine.backend,
	} {
		if err := closer.Close(); err != nil {
			err = errors.Join(err, errnie.Error(err))
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
Cycle submits READY Values against the staging lanes their programs wrote into
the backend, syncs, and repeats until no Value is left to dispatch.

Submissions that touch disjoint owner/lane frame sets run together. Overlapping
sets are split across batches so two kernels never mutate the same Value frame
at the same time.
*/
func (machine *Machine) Cycle() (
	resolved []*primitive.Value,
	ready []*primitive.Value,
	err error,
) {
	if machine.backend == nil {
		return nil, nil, errors.New("backend is nil")
	}

	resolved = make([]*primitive.Value, 0)
	ready = make([]*primitive.Value, 0)

	for result := range machine.backend.Sync(machine.ctx) {
		resolved = append(resolved, result.Resolved...)
		ready = append(ready, result.Ready...)
	}

	return resolved, ready, err
}

/*
Load ingests the dataset and seeds the bootstrap: the query is staged with the
full community, and the recruiter waits for the query's reference markers to
turn into its own pool. Cycle then drains the chain end-to-end.
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
			io.Copy(machine.telemetry, segment)
		}
	}

	for _, value := range machine.firmware.Deploy(
		core.RECRUIT_COMMUNITY, []uint64{
			uint64(primitive.PropertyWord(primitive.COMMUNITY)), 0,
		},
		nil,
	) {
		machine.backend.Submit(value)
		io.Copy(machine.telemetry, value)
	}

	ready := []*primitive.Value{nil}

	for len(ready) > 0 {
		if _, ready, err = machine.Cycle(); err != nil {
			return err
		}

		for _, value := range ready {
			io.Copy(machine.telemetry, value)
		}
	}

	return nil
}

/*
Prompt injects prompt segment Values into the community, cycles until settled,
and returns Values that newly settled in the RESOLVED or DONE state.
*/
func (machine *Machine) Prompt(
	values ...*primitive.Value,
) (resolved []*primitive.Value, err error) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errors.Join(machine.err, errnie.Error(err))
	}

	resolved = make([]*primitive.Value, 0)

	for len(resolved) == 0 {
		if resolved, _, err = machine.Cycle(); err != nil {
			return nil, err
		}

		for _, value := range resolved {
			io.Copy(machine.telemetry, value)
		}
	}

	return resolved, nil
}
