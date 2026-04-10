package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/kadabra"
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
	kadabra   *kadabra.Node
	peers     []*kadabra.Node
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

	if machine.queue, machine.err = pool.NewQueue(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	machine.backend = compute.NewBackend(
		ctx,
		machine.queue,
		compute.WithExploreEvery(128),
	)

	if machine.kadabra, machine.err = kadabra.NewNode(
		ctx,
		machine.host.Name,
		machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	machine.kadabra.SetMeshExpandHandler(func(incoming *primitive.Affinity) bool {
		return machine.meshExpandDuringLoad(incoming)
	})

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx, machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"kadabra":   machine.kadabra,
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

	for _, peer := range machine.peers {
		if err := peer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.kadabra != nil {
		if err := machine.kadabra.Close(); err != nil {
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
meshExpandDuringLoad materializes another Kadabra node and rewires a full
mesh among the primary, existing peers, and the newcomer when the primary’s
ingest centroid saturates at ShannonLimit. Affinity is passed through for
future shard-aware placement; peer count obeys MaxMeshPeers.
*/
func (machine *Machine) meshExpandDuringLoad(incoming *primitive.Affinity) bool {
	_ = incoming

	if len(machine.peers) >= core.Cfg.Kadabra.MaxMeshPeers {
		errnie.Warn(
			"machine: dynamic mesh peer cap reached",
			"peer_count", len(machine.peers),
			"cap", core.Cfg.Kadabra.MaxMeshPeers,
		)

		return true
	}

	peer, peerErr := kadabra.NewNode(
		machine.ctx,
		fmt.Sprintf("peer-%d", len(machine.peers)+1),
		machine.queue,
	)

	if peerErr != nil {
		errnie.Warn("machine: dynamic mesh peer failed", "err", peerErr)

		return true
	}

	backbone := append([]*kadabra.Node{machine.kadabra}, machine.peers...)

	for _, existing := range backbone {
		kadabra.Connect(peer, existing, 1.0)
	}

	machine.peers = append(machine.peers, peer)

	return true
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
	if err := validate.Require(map[string]any{
		"kadabra":   machine.kadabra,
		"queue":     machine.queue,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	pipeline, err := transport.NewPipeline(
		machine.ctx,
		false,
		machine.tokenizer,
		machine.kadabra,
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
		"kadabra":   machine.kadabra,
		"queue":     machine.queue,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	if provider == nil {
		return errnie.Error(fmt.Errorf("vm.Machine.LoadPrompts: nil PromptProvider"))
	}

	publishers := []transport.Publishable{
		machine.kadabra,
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
func (machine *Machine) Prompt(prompt string) (prediction *algo.Prediction, err error) {
	if err := validate.Require(map[string]any{
		"kadabra": machine.kadabra,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	values, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	defer primitive.CloseAll(values)

	value := values[len(values)-1]

	if prediction, err = machine.kadabra.Predict(value); err != nil {
		return nil, errnie.Error(err)
	}

	return prediction, nil
}
