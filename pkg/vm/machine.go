package vm

import (
	"bytes"
	"context"
	"os"
	"strings"

	"github.com/theapemachine/six/experiment/data"
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
		machine.kadabra = kadabra.NewKadabraNode(
			machine.defaultNodeID(),
		)
	}

	return machine, validate.Require(map[string]any{
		"ctx":    machine.ctx,
		"cancel": machine.cancel,
	})
}

/*
Run starts the machine and ingests the dataset into the Kadabra node.
Before we can prompt the machine, we need to have the full dataset.
*/
func (machine *Machine) Run() error {
	if err := validate.Require(map[string]any{
		"machine": machine,
		"ctx":     machine.ctx,
		"cancel":  machine.cancel,
		"dataset": machine.dataset,
		"kadabra": machine.kadabra,
	}); err != nil {
		return errnie.Error(err)
	}

	defer func() {
		errnie.Error(
			machine.dataset.Close(),
		)
	}()

	buffer := bytes.NewBuffer(make([]byte, 0, defaultMachineChunkBytes))

	for b := range machine.dataset.Generate() {
		buffer.WriteByte(b)

		if buffer.Len() < defaultMachineChunkBytes {
			continue
		}

		value, err := primitive.NewValue(buffer.Bytes())

		if err != nil {
			return errnie.Error(
				NewVmError(ErrVmInvalidValue, err, "NewValue"),
				"buffer", buffer.Bytes(),
			)
		}

		if err := value.ComputeAffinityLSH(); err != nil {
			_ = value.Close()
			return errnie.Error(
				NewVmError(ErrVmInvalidValue, err, "ComputeAffinityLSH"),
				"buffer", buffer.Bytes(),
			)
		}

		if _, err := machine.kadabra.Publish(*value, machine.label); err != nil {
			return errnie.Error(
				NewVmError(ErrVmInvalidSequence, err, "publishSequence"),
				"buffer", buffer.Bytes(),
			)
		}

		_ = value.Close()
		buffer.Reset()
	}

	if buffer.Len() > 0 {
		value, err := primitive.NewValue(buffer.Bytes())

		if err != nil {
			return errnie.Error(
				NewVmError(ErrVmInvalidValue, err, "NewValue"),
				"buffer", buffer.Bytes(),
			)
		}

		if err := value.ComputeAffinityLSH(); err != nil {
			_ = value.Close()
			return errnie.Error(
				NewVmError(ErrVmInvalidValue, err, "ComputeAffinityLSH"),
				"buffer", buffer.Bytes(),
			)
		}

		if _, err := machine.kadabra.Publish(*value, machine.label); err != nil {
			return errnie.Error(
				NewVmError(ErrVmInvalidSequence, err, "publishSequence"),
				"buffer", buffer.Bytes(),
			)
		}

		_ = value.Close()
	}

	return nil
}

/*
Prompt the machine and retrieve both a prediction and a classification.
*/
func (machine *Machine) Prompt(prompt string) (
	generation string, classification map[string]float64,
) {
	if machine == nil || machine.kadabra == nil || machine.kadabra.Store == nil {
		return "", nil
	}

	return machine.kadabra.Store.Generate(
			prompt, machine.label, 0.5, 100,
		),
		machine.kadabra.Store.Classify(
			prompt,
		)
}

func (machine *Machine) defaultNodeID() kadabra.NodeID {
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
