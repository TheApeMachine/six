package vm

import (
	"context"
	"errors"
	"io"
	"sync"

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
	community sync.Map
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
		ctx:       ctx,
		cancel:    cancel,
		telemetry: bridge,
		backend:   compute.NewBackend(ctx),
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

	machine.community.Range(func(key, value any) bool {
		if value != nil {
			value.(*primitive.Value).Close()
		}

		return true
	})

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
Cycle is a dumb scheduler: it submits every Value whose program is READY
against the staging lane its program (or another program upstream) wrote
into the backend, syncs, and repeats until no Value is left to dispatch.
The host makes no decisions about what goes where — programs do, by
emitting stage instructions inside the kernel.
*/
func (machine *Machine) Cycle() (err error) {
	if machine.backend == nil {
		return nil
	}

	for {
		select {
		case <-machine.ctx.Done():
			return machine.ctx.Err()
		default:
		}

		submitted := 0

		type submission struct {
			owner *primitive.Value
			lane  []*primitive.Value
		}

		var submissions []submission

		machine.community.Range(func(key, value any) bool {
			owner := value.(*primitive.Value)

			if !owner.ReadyForALU() {
				return true
			}

			lane := machine.backend.Lane(owner)
			if len(lane) == 0 {
				return true
			}

			if err = machine.backend.Submit(owner, lane); err != nil {
				return false
			}

			submissions = append(submissions, submission{owner: owner, lane: lane})
			submitted++
			return true
		})

		if err != nil {
			return err
		}

		if submitted == 0 {
			return nil
		}

		for spawned := range machine.backend.Sync(machine.ctx) {
			machine.community.Store(spawned.ID(), spawned)
			io.Copy(machine.telemetry, spawned)
		}

		// Emit telemetry for every submitted owner and every member of
		// the lane it just consumed: gossip writes mutate peer frames
		// (community markers, affinity merges, status flags) so the
		// visualiser only sees community membership form if those peers
		// re-publish after the sweep.
		for _, sub := range submissions {
			io.Copy(machine.telemetry, sub.owner)

			for _, peer := range sub.lane {
				io.Copy(machine.telemetry, peer)
			}
		}
	}
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
		_ = machine.telemetry.BeginRun()
	}

	var loaded []*primitive.Value

	for sample := range dataset.Generate() {
		segments, err := machine.tokenizer.IngestSample(
			machine.ctx, sample,
		)

		if err != nil {
			return errors.Join(machine.err, errnie.Error(err))
		}

		for _, segment := range segments {
			machine.community.Store(segment.ID(), segment)
			loaded = append(loaded, segment)
			io.Copy(machine.telemetry, segment)
		}
	}

	recruiter := primitive.Emit(
		primitive.WithFirmware(core.RECRUIT_COMMUNITY),
	)

	query := primitive.Emit(
		primitive.WithFirmware(core.QUERY),
		primitive.WithReference(recruiter.ID()),
	)

	machine.community.Store(recruiter.ID(), recruiter)
	machine.community.Store(query.ID(), query)
	io.Copy(machine.telemetry, recruiter)
	io.Copy(machine.telemetry, query)

	for _, segment := range loaded {
		machine.backend.StageInto(query.ID(), segment)
	}

	return machine.Cycle()
}

/*
Prompt injects prompt segment Values into the community, cycles until settled,
and returns Values that newly settled in the RESOLVED or DONE state.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (resolved []*primitive.Value, err error) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errors.Join(machine.err, errnie.Error(err))
	}

	var community []*primitive.Value
	settled := make(map[uint64]struct{})

	machine.community.Range(func(key, value any) bool {
		member := value.(*primitive.Value)

		switch member.Status() {
		case primitive.RESOLVED, primitive.DONE:
			settled[member.ID()] = struct{}{}
		}

		if member.Role() != primitive.ValueRolePrompt {
			community = append(community, member)
		}

		return true
	})

	for _, value := range values {
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		machine.community.Store(value.ID(), value)
	}

	for _, value := range values {
		if !value.ReadyForALU() {
			continue
		}

		for _, prompt := range values {
			machine.backend.StageInto(value.ID(), prompt)
		}

		for _, member := range community {
			machine.backend.StageInto(value.ID(), member)
		}
	}

	if err = machine.Cycle(); err != nil {
		return nil, err
	}

	machine.community.Range(func(key, value any) bool {
		member := value.(*primitive.Value)
		switch member.Status() {
		case primitive.RESOLVED, primitive.DONE:
			if _, ok := settled[member.ID()]; ok {
				return true
			}

			resolved = append(resolved, member)
		}
		return true
	})

	return resolved, nil
}
