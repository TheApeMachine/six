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

/*
PromptResolution captures the low-level halt state and the higher-level
resolution snapshot for one prompt run. ExecutionSettled is the scheduler
fact; ReasoningResolved is the semantic contract experiment code should read.
*/
type PromptResolution struct {
	Value             *primitive.Value
	Generation        string
	PropertiesWord    uint64
	ProbeState        uint64
	ProbeDepth        uint64
	ExecutionSettled  bool
	ReasoningResolved bool
	HaltedByCeiling   bool
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
		conn:    gossip.NewConn(ctx),
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
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then publishes through the queue and
orchestrator. Resets tokenizer ingest state when finished so later Prompt paths
see a clean pipe.
*/
func (machine *Machine) Load(dataset data.Provider) error {
	if err := validate.Require(map[string]any{
		"queue":     machine.queue,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	publishers := []transport.Publishable{
		machine.queue,
		machine.orchestrator,
	}

	for sample := range dataset.Generate() {
		if ingestErr := machine.tokenizer.IngestSample(
			machine.ctx, sample, publishers,
		); ingestErr != nil {
			return errnie.Error(ingestErr)
		}
	}

	machine.tokenizer.ResetAfterEOF()
	machine.orchestrator.Label()

	return nil
}

/*
Prompt feeds the prompt string through the same path as Load — tokenize,
link, install affinity, publish as tracked. We spin until the Value's
scheduling word is zeroed (meaning it has settled), then return it.
Causal children the queue spawns are irrelevant — they keep running in
the background and their traces accumulate in the field.
*/
func (machine *Machine) Prompt(value *primitive.Value) ([]*primitive.Value, error) {
	if err := validate.Require(map[string]any{
		"value": value,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.PromptEvent(string(value.Bytes())))
	}

	return machine.orchestrator.Cycle(value)
}

/*
PromptWithResolution tokenizes a prompt string, installs the named program,
runs it through the orchestrator cycle, and wraps the result into a
PromptResolution suitable for experiment scoring.
*/
func (machine *Machine) PromptWithResolution(
	prompt string, program string,
) (*PromptResolution, error) {
	segments, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	if len(segments) == 0 {
		return nil, errnie.Error(errors.New("vm.Machine.PromptWithResolution: empty prompt"))
	}

	for _, seg := range segments {
		installer := programmer.Installer{}

		if installErr := installer.InstallProgram(seg, program); installErr != nil {
			return nil, errnie.Error(installErr)
		}
	}

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.PromptEvent(prompt))
	}

	resolved, cycleErr := machine.orchestrator.Cycle(segments...)

	if cycleErr != nil {
		return nil, cycleErr
	}

	resolution := &PromptResolution{}

	if len(resolved) > 0 && resolved[0] != nil {
		value := resolved[0]
		resolution.Value = value
		resolution.Generation = value.String()
		resolution.PropertiesWord = (*value)[kernel.PropertiesStartWord]
		resolution.ProbeState = (*value)[kernel.PropertiesProbeStateWord]
		resolution.ProbeDepth = (*value)[kernel.PropertiesProbeDepthWord]
		resolution.ExecutionSettled = true
		resolution.ReasoningResolved = true
	} else if len(segments) > 0 {
		value := segments[0]
		resolution.Value = value
		resolution.Generation = value.String()
		resolution.PropertiesWord = (*value)[kernel.PropertiesStartWord]
		resolution.ProbeState = (*value)[kernel.PropertiesProbeStateWord]
		resolution.ProbeDepth = (*value)[kernel.PropertiesProbeDepthWord]
		resolution.ExecutionSettled = true
	}

	return resolution, nil
}
