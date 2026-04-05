package vm

import (
	"bytes"
	"context"
	"os"
	"strings"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/kadabra"
)

const (
	defaultMachineChunkBytes = 64
	defaultMachineLabel      = "Machine"
)

/*
Machine is a central orchestrator that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx     context.Context
	cancel  context.CancelFunc
	err     error
	dataset data.Provider
	kadabra *kadabra.KadabraNode
	label   string
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	machine := &Machine{
		ctx:    ctx,
		cancel: cancel,
		label:  defaultMachineLabel,
	}

	for _, opt := range opts {
		opt(machine)
	}

	if machine.kadabra == nil {
		machine.kadabra = kadabra.NewKadabraNode(machine.defaultNodeID())
	}

	return machine, validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
	})
}

func (machine *Machine) Run() error {
	if err := validate.Require(map[string]any{
		"machine": machine,
		"ctx":     machine.ctx,
		"cancel":  machine.cancel,
		"dataset": machine.dataset,
		"kadabra": machine.kadabra,
	}); err != nil {
		return err
	}

	defer func() {
		errnie.Error(
			machine.dataset.Close(),
		)
	}()

	if promptDataset, ok := machine.dataset.(data.PromptProvider); ok {
		for prompt := range promptDataset.GeneratePrompts() {
			label := machine.label
			if prompt.HasLabel && strings.TrimSpace(prompt.Label) != "" {
				label = strings.TrimSpace(prompt.Label)
			}

			if err := machine.publishSequence(prompt.Text, label); err != nil {
				return err
			}
		}

		return nil
	}

	buffer := bytes.NewBuffer(make([]byte, 0, defaultMachineChunkBytes))

	for b := range machine.dataset.Generate() {
		buffer.WriteByte(b)

		if buffer.Len() < defaultMachineChunkBytes {
			continue
		}

		if err := machine.publishChunk(buffer.Bytes()); err != nil {
			return err
		}

		buffer.Reset()
	}

	if buffer.Len() == 0 {
		return nil
	}

	return machine.publishSequence(buffer.String(), machine.label)
}

func (machine *Machine) Prompt(prompt string) error {
	return machine.publishSequence(prompt, machine.label)
}

/*
Kadabra returns the local Kadabra DHT node used by the machine.
*/
func (machine *Machine) Kadabra() *kadabra.KadabraNode {
	if machine == nil {
		return nil
	}

	return machine.kadabra
}

func (machine *Machine) publishChunk(chunk []byte) error {
	return machine.publishSequence(string(chunk), machine.label)
}

func (machine *Machine) publishSequence(sequence string, label string) error {
	if sequence == "" {
		return nil
	}

	val, err := primitive.NewValue([]byte(sequence))
	if err != nil {
		return err
	}

	defer func() {
		errnie.Error(val.Close())
	}()

	if _, err := machine.kadabra.Publish(sequence, label); err != nil {
		return err
	}

	return nil
}

func (machine *Machine) defaultNodeID() kadabra.NodeID {
	if core.Cfg != nil && core.Cfg.ControlPlane.NodeID != 0 {
		return kadabra.NodeID(core.Cfg.ControlPlane.NodeID)
	}

	hostname, err := os.Hostname()

	if err == nil && hostname != "" {
		return kadabra.NodeIDFromString(hostname)
	}

	return kadabra.NodeIDFromString("six-machine")
}

/*
WithDataset sets the dataset for the machine.
*/
func MachineWithDataset(dataset data.Provider) machineOpts {
	return func(machine *Machine) {
		machine.dataset = dataset
	}
}

/*
MachineWithKadabraNode sets the Kadabra node used for sequence publishing.
*/
func MachineWithKadabraNode(node *kadabra.KadabraNode) machineOpts {
	return func(machine *Machine) {
		machine.kadabra = node
	}
}

/*
MachineWithKadabraLabel sets the fallback label used for unlabeled sequences.
*/
func MachineWithKadabraLabel(label string) machineOpts {
	return func(machine *Machine) {
		label = strings.TrimSpace(label)

		if label == "" {
			return
		}

		machine.label = label
	}
}
