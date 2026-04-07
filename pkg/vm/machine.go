package vm

import (
	"context"
	"errors"
	"fmt"
	"io"

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
)

const maxMeshPeers = 4096

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

	machine.backend = compute.NewBackend(ctx, machine.queue)
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

		if wantPeers > maxMeshPeers {
			effective = maxMeshPeers

			errnie.Warn(
				"machine: meshSize trimmed to peer cap",
				"requested_mesh_size", machine.meshSize,
				"effective_peer_count", effective,
				"cap", maxMeshPeers,
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
*/
func (machine *Machine) Close() error {
	machine.cancel()

	var errs []error

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

When the dataset implements PromptProvider, structured samples with
labels are published directly to kadabra. Otherwise raw bytes are
streamed through the tokenizer and each resulting Value is published
unlabeled.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"kadabra":   machine.kadabra,
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	// Structured path: labeled prompts go straight to kadabra.
	if pp, ok := dataset.(data.PromptProvider); ok {
		allNodes := machine.allNodes()
		published := 0

		for prompt := range pp.GeneratePrompts() {
			select {
			case <-machine.ctx.Done():
				return machine.ctx.Err()
			default:
			}

			value, vErr := primitive.NewValue([]byte(prompt.Text))

			if vErr != nil {
				errnie.Debug("machine.Load: skipping bad value", "err", vErr)
				continue
			}

			value.ComputeAffinityLSH()

			// Round-robin across mesh nodes.
			target := allNodes[published%len(allNodes)]

			if _, pErr := target.Publish(value, prompt.Label); pErr != nil {
				errnie.Debug("machine.Load: publish failed", "err", pErr)
				continue
			}

			published++
		}

		// Give pool workers time to finish Store dispatches before gossip
		// triggers field dynamics that also touch the compute backend.
		machine.queue.Drain()
		machine.propagateGossip()

		return nil
	}

	// Unstructured path: tokenize raw bytes, publish each Value.
	buf := make([]byte, 1024)

	for {
		select {
		case <-machine.ctx.Done():
			return machine.ctx.Err()
		default:
		}

		n, wErr := io.CopyBuffer(machine.tokenizer, dataset, buf)

		if wErr != nil {
			if errors.Is(wErr, io.EOF) {
				break
			}

			return errnie.Error(wErr)
		}

		if n == 0 {
			break
		}

		// Read back the tokenized Value.
		readBuf := make([]byte, int(core.Cfg.Value.Bytes))

		rn, rErr := machine.tokenizer.Read(readBuf)

		if rErr != nil || rn == 0 {
			errnie.Debug(
				"machine.Load: tokenizer read short or error",
				"err", rErr, "rn", rn,
			)
			continue
		}

		value, vErr := primitive.NewValue(readBuf[:rn])

		if vErr != nil {
			errnie.Debug(
				"machine.Load: skipping bad value",
				"err", vErr, "rn", rn,
			)
			continue
		}

		value.ComputeAffinityLSH()

		if _, pErr := machine.kadabra.Publish(value, ""); pErr != nil {
			errnie.Debug("machine.Load: raw publish failed", "err", pErr)
		}
	}

	return nil
}

/*
Prompt the machine and retrieve both a prediction and a classification.

The prompt is converted into a temporary Value so we can compute its
affinity vector, which the Kadabra node uses to route the query to the
closest trie cluster(s). This ensures the prompt reaches the trie that
holds the most relevant data.
*/
func (machine *Machine) Prompt(prompt string) (*algo.Prediction, error) {
	if err := validate.Require(map[string]any{
		"kadabra": machine.kadabra,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	value, err := primitive.NewValue([]byte(prompt))

	if err != nil {
		return nil, errnie.Error(err)
	}

	return machine.kadabra.Predict(value)
}

/*
allNodes returns the primary kadabra node plus all mesh peers.
*/
func (machine *Machine) allNodes() []*kadabra.Node {
	nodes := make([]*kadabra.Node, 0, 1+len(machine.peers))
	nodes = append(nodes, machine.kadabra)
	nodes = append(nodes, machine.peers...)

	return nodes
}

/*
propagateGossip runs one round of digest exchange across all mesh nodes
so field dynamics, eigenmode detection, and pressure events fire.

Each node first self-absorbs its own trie digests so the intra-node
field (trie-to-trie coupling, eigenmode detection, asymmetric pressure)
can operate even with a single node. Then digests propagate to peers.
*/
func (machine *Machine) propagateGossip() {
	allNodes := machine.allNodes()

	for _, node := range allNodes {
		digests := node.Gossip().Digests()

		// Self-absorb: the field needs its own trie digests to
		// compute coupling, detect eigenmodes, and apply pressure.
		for _, digest := range digests {
			node.Field.Absorb(digest)
		}

		for _, peer := range allNodes {
			if peer.ID == node.ID {
				continue
			}

			for _, digest := range digests {
				peer.Field.Absorb(digest)
			}
		}
	}
}
