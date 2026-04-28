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
Cycle submits READY Values against the staging lanes their programs wrote into
the backend, syncs, and repeats until no Value is left to dispatch.

Submissions that touch disjoint owner/lane frame sets run together. Overlapping
sets are split across batches so two kernels never mutate the same Value frame
at the same time.
*/
func (machine *Machine) Cycle() (err error) {
	if machine.backend == nil {
		return nil
	}

	machine.wakeWaiting()

	machine.community.Range(func(key, value any) bool {
		if value.(*primitive.Value).Status() == primitive.READY {
			io.Copy(machine.telemetry, value.(*primitive.Value))
			machine.backend.Submit(value.(*primitive.Value))
		}

		return true
	})

	for value := range machine.backend.Sync(machine.ctx) {
		machine.community.Store(value.ID(), value)
		io.Copy(machine.telemetry, value)
	}

	return errors.Join(machine.err, errnie.Error(err))
}

/*
wakeWaiting flips WAITING values whose id is the continuation target of
some DONE value into READY. This is the in-band handshake firmware uses
to chain stages: a query Value runs, stamps the recruiter's id into its
own continuation when it finishes, and the next Cycle pass sees "this
DONE Value points at WAITING Value X — wake X so the next submission
sweep picks it up". A self-pointing continuation is the "re-run me"
signal handled inside backend.finishOwner; it never wakes another
value.
*/
func (machine *Machine) wakeWaiting() {
	machine.community.Range(func(_, value any) bool {
		owner := value.(*primitive.Value)

		if owner.Status() != primitive.DONE {
			return true
		}

		next, ok := machine.community.Load(owner.SchedulingNext())

		if !ok {
			return true
		}

		next.(*primitive.Value).SetStatus(primitive.READY)
		io.Copy(machine.telemetry, next.(*primitive.Value))

		return true
	})
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
		primitive.WithStatus(uint64(primitive.WAITING)),
	)

	// The query firmware filters peers via `B.{{A.context[0,1]}} ==
	// {{A.context[1,1]}}`. WithContext writes the operand offset and
	// comparison literal BEFORE WithFirmware so InstallFirmware can
	// patch the predicate's packed instruction with the resolved
	// offset. For bootstrap recruitment we want B.properties.community
	// (absolute word = propertiesStart + COMMUNITY offset) compared
	// against 0 (the unclaimed sentinel).
	communityWord := uint64(core.Cfg.Value.Region.Properties.Start + int(primitive.COMMUNITY))

	query := primitive.Emit(
		primitive.WithReference(recruiter.ID()),
		primitive.WithContext(0, communityWord),
		primitive.WithContext(1, 0),
		primitive.WithFirmware(core.QUERY),
		primitive.WithStatus(uint64(primitive.READY)),
	)

	machine.community.Store(recruiter.ID(), recruiter)
	machine.community.Store(query.ID(), query)

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

	resolved = make([]*primitive.Value, 0)
	done := false

	for !done {
		select {
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()
		default:
			if err := machine.Cycle(); err != nil {
				return nil, err
			}

			// Check for resolved values
			for _, value := range values {
				if value.Status() == primitive.RESOLVED {
					resolved = append(resolved, value)
				}
			}

			if len(resolved) == len(values) {
				done = true
			}
		}
	}

	return resolved, nil
}
