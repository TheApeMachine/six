package vm

import (
	"context"
	"errors"
	"fmt"

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
	meshSize  int
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
	machine.queue.SetBackend(machine.backend)

	if machine.kadabra, machine.err = kadabra.NewNode(
		ctx,
		machine.host.Name,
		machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx, machine.queue,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	// Spin up peer nodes and wire a full mesh for gossip/field dynamics.
	if machine.meshSize > 1 {
		wantPeers := machine.meshSize - 1
		effective := wantPeers

		if wantPeers > core.Cfg.Kadabra.MaxMeshPeers {
			effective = core.Cfg.Kadabra.MaxMeshPeers

			errnie.Warn(
				"machine: meshSize trimmed to peer cap",
				"requested_mesh_size", machine.meshSize,
				"effective_peer_count", effective,
				"cap", core.Cfg.Kadabra.MaxMeshPeers,
			)
		}

		allNodes := []*kadabra.Node{machine.kadabra}

		for idx := 0; idx < effective; idx++ {
			name := fmt.Sprintf("peer-%d", idx+1)
			peer, pErr := kadabra.NewNode(ctx, name, machine.queue)

			if pErr != nil {
				errnie.Warn("machine: peer node failed", "name", name, "err", pErr)
				continue
			}

			machine.peers = append(machine.peers, peer)
			allNodes = append(allNodes, peer)
		}

		for idx := range allNodes {
			for jdx := idx + 1; jdx < len(allNodes); jdx++ {
				kadabra.Connect(allNodes[idx], allNodes[jdx], 1.0)
			}
		}
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
MachineWithMesh creates additional kadabra peer nodes and wires
them into a full mesh so gossip, field dynamics, and replication
events flow between nodes during operation.
*/
func MachineWithMesh(size int) machineOpts {
	return func(machine *Machine) {
		if size < 2 {
			size = 2
		}

		machine.meshSize = size
	}
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
insert has attempted to complete before the method returns (same queue
covers mesh peers wired to this Machine).
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
	)

	if err != nil {
		return errnie.Error(err)
	}

	loadErr := errnie.Error(pipeline.LoadFrom(dataset))

	machine.queue.Drain()

	return loadErr
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

	value, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	defer value.Close()

	if prediction, err = machine.kadabra.Predict(value); err != nil {
		return nil, errnie.Error(err)
	}

	return prediction, nil
}
