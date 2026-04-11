package vm

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
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
	queue     *pool.Queue
	backend   *compute.Backend
	tokenizer *Tokenizer
	conn      *gossip.Conn
	field     *geometry.Field
	remSleep  *time.Ticker
	remDone   chan struct{}
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

	machine.backend = compute.NewBackend(
		ctx,
		machine.queue,
		compute.WithExploreEvery(128),
	)

	machine.conn = gossip.NewConn(nil, nil)
	machine.field = geometry.NewField(geometry.Mod8191)

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx, machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	machine.remSleep = time.NewTicker(10 * time.Second)
	go machine.runREMSleep()

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
Load a dataset into the machine.
No assumptions are made about the incoming data at this stage to mimick
real-world data streaming, which may not provide things like boundaries
labels, or sample IDs. transport.Pipeline runs tokenizer ingest concurrently
with DrainPublishedValues, which publishes each minted *Value directly to
Kadabra (no tokenizer → wire → ValueFromWireFrame round trip).

kadabra.Publish snapshots each Value into a SequenceRecord and schedules
the trie insert and replication fan-out on the shared pool.Queue. Load
therefore calls Queue.Drain after the pipeline finishes so every scheduled
insert has attempted to complete before the method returns (the shared queue
also serves peers added dynamically when ingest reaches ShannonLimit).
*/
func (machine *Machine) Load(dataset data.Provider) error {
	if promptProvider, ok := dataset.(data.PromptProvider); ok {
		return machine.LoadPrompts(promptProvider)
	}

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
	)

	if err != nil {
		return errnie.Error(err)
	}

	loadErr := errnie.Error(pipeline.LoadFrom(dataset))
	machine.queue.Drain()

	return loadErr
}

/*
LoadPrompts ingests structured samples: each Prompt.Text is chunked like a
raw byte stream, and every chunk from that text is Published with the same
label when HasLabel is set (empty string otherwise). The tokenizer ring is
reused between samples via IngestReader, so performance matches repeated
close-and-drain cycles without a second goroutine.

Do not call LoadPrompts concurrently with Load on the same Machine; both
use the shared Tokenizer.
*/
func (machine *Machine) LoadPrompts(provider data.PromptProvider) error {
	if err := validate.Require(map[string]any{
		"queue":     machine.queue,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	if provider == nil {
		return errnie.Error(fmt.Errorf("vm.Machine.LoadPrompts: nil PromptProvider"))
	}

	publishers := []transport.Publishable{
		machine.queue,
	}

	for prompt := range provider.GeneratePrompts() {
		label := ""

		if prompt.HasLabel {
			label = prompt.Label
		}

		ingestErr := machine.tokenizer.IngestReader(
			machine.ctx,
			strings.NewReader(prompt.Text),
			label,
			publishers,
			nil,
		)

		if ingestErr != nil {
			return errnie.Error(ingestErr)
		}

		machine.queue.Drain()
	}

	machine.queue.Drain()

	return nil
}

/*
Prompt the machine and retrieve both a prediction and a classification.

The prompt is converted into a Value via NewValue, which derives the
affinity vector Kadabra uses to route the query to the closest trie
cluster(s).
*/
func (machine *Machine) runREMSleep() {
	for {
		select {
		case <-machine.remDone:
			return
		case <-machine.remSleep.C:
			// Inject random noise values for consolidation
			machine.injectREMNoise()
		}
	}
}

func (machine *Machine) injectREMNoise() {
	if machine.queue == nil {
		return
	}

	// Generate a random noise payload
	noise := make([]byte, 32)

	values, err := primitive.NewValue(noise)
	if err != nil {
		return
	}

	for _, v := range values {
		// Set a low TTL for ephemeral exploration
		v.Set(51, 0xFF) // meta[3] / MetaTTLWord

		// Seed temperature noise into meta[4] (word 52)
		noiseMask := rand.Uint64()
		v.Set(52, noiseMask)

		_ = machine.queue.Publish(v, "rem_sleep")
	}
}

/*
Prompt the machine and retrieve both a prediction and a classification.

The prompt is converted into a Value via NewValue, which derives the
affinity vector Kadabra uses to route the query to the closest trie
cluster(s).
*/
func (machine *Machine) Prompt(prompt string) (*primitive.Value, error) {
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

	if err = machine.queue.Publish(value, "prompt"); err != nil {
		primitive.CloseAll(values)
		return nil, errnie.Error(err)
	}

	// We return the value so the caller can inspect it,
	// but we don't close it here because it's now in the queue.
	return value, nil
}
