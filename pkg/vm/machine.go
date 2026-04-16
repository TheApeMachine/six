package vm

import (
	"context"
	"errors"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Machine is a central orchestrator that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	host         *network.Host
	tokenizer    *Tokenizer
	orchestrator *Orchestrator
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	machine := &Machine{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.orchestrator, machine.err = NewOrchestrator(
		ctx,
	); machine.err != nil {
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
	})
}

/*
Close the machine.

Cancels the shared pool.Queue (owned here for Backend construction) once host
and tokenizer are closed so goroutine-pool work does not outlive dependents.
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

	if machine.orchestrator != nil {
		if err := machine.orchestrator.Close(); err != nil {
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
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then runs orchestrator.Publish per
segment. Resets tokenizer ingest state when finished so later Prompt paths
see a clean pipe.
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

		machine.orchestrator.Cycle(segments...)
	}

	return nil
}

/*
Prompt injects the prompt segment Values on the first orchestrator Cycle, then
runs further Cycles with no new ingress until the field reports at least one
resolved Value (belief gap ≤ BeliefEpsilon — see Orchestrator.Cycle). Those
returned Values are the prompt outcome. The only normal exit is gap closure;
use context cancellation or deadline on NewMachine’s context to bound work if
the substrate never reaches epsilon.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (
	resolved []*primitive.Value, err error,
) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	return machine.orchestrator.Cycle(values...)
}
