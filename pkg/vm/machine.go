package vm

import (
	"context"
	"errors"
	"time"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
	"github.com/theapemachine/six/pkg/viz"
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
	queue        *pool.Queue
	backend      *compute.Backend
	tokenizer    *Tokenizer
	conn         *gossip.Conn
	field        *geometry.Field
	orchestrator *Orchestrator
	programReady chan struct{}
	remSleep     *time.Ticker
	remDone      chan struct{}
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	machine := &Machine{
		ctx:     ctx,
		cancel:  cancel,
		remDone: make(chan struct{}),
		field:   geometry.NewField(geometry.Mod65537),
		conn:    gossip.NewConn(nil, nil),
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.queue, machine.err = pool.NewQueue(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	machine.programReady = make(chan struct{}, 1)

	machine.orchestrator, machine.err = NewOrchestrator(ctx, machine.conn, machine.queue, machine.programReady)
	if machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	machine.backend = compute.NewBackend(
		ctx,
		machine.queue,
		compute.WithExploreEvery(128),
	)

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx, machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"queue":     machine.queue,
		"backend":   machine.backend,
		"tokenizer": machine.tokenizer,
	})
}

/*
Close the machine.

Outstanding queue work (trie/replica tasks scheduled from Publish) is
allowed to finish before contexts are cancelled so shutdown matches the
post-Load guarantee as closely as practical for a shared Queue.
*/
func (machine *Machine) Close() error {
	var errs []error

	if machine.queue != nil {
		machine.queue.Drain()
	}

	if machine.remSleep != nil {
		machine.remSleep.Stop()
		close(machine.remDone)
	}

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

	if machine.queue != nil {
		if err := machine.queue.Close(); err != nil {
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
Load a dataset into the machine. Every Value goes through the same path:
tokenizer chunks the byte stream, links Values via prev/next, installs
the affinity program, and publishes to the queue.
*/
func (machine *Machine) Load(dataset data.Provider) error {
	if err := validate.Require(map[string]any{
		"queue":     machine.queue,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	pipeline, err := transport.NewPipeline(
		machine.ctx,
		false,
		machine.tokenizer,
		machine.queue,
		machine.orchestrator,
	)

	if err != nil {
		return errnie.Error(err)
	}

	loadErr := errnie.Error(pipeline.LoadFrom(dataset))
	machine.queue.Drain()

	return loadErr
}

/*
Prompt feeds the prompt string through the same path as Load — tokenize,
link, install affinity, publish as tracked. We spin until the Value's
scheduling word is zeroed (meaning it has settled), then return it.
Causal children the queue spawns are irrelevant — they keep running in
the background and their traces accumulate in the field.
*/
func (machine *Machine) Prompt(prompt string, program string) (*primitive.Value, error) {
	if err := validate.Require(map[string]any{
		"queue": machine.queue,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	values, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	value := values[len(values)-1]

	installer := programmer.Installer{}

	if installErr := installer.InstallProgram(value, program); installErr != nil {
		return nil, errnie.Error(installErr)
	}

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.PromptEvent(prompt))
	}

	if err = machine.queue.PublishTracked(value, "prompt"); err != nil {
		return nil, errnie.Error(err)
	}

	if err = machine.orchestrator.Publish(value, "prompt"); err != nil {
		return nil, errnie.Error(err)
	}

	poll := time.NewTicker(500 * time.Microsecond)

	defer poll.Stop()

	for {
		select {
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()

		case <-machine.programReady:
			if value[kernel.SchedulingNextProgramWord] == 0 {
				return value, nil
			}

		case <-poll.C:
			if value[kernel.SchedulingNextProgramWord] == 0 {
				return value, nil
			}
		}
	}
}
