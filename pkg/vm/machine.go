package vm

import (
	"io"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/workflow"
)

type Machine struct {
	dataset io.ReadCloser
	seed    io.ReadWriteCloser
	prompt  io.ReadWriteCloser
	backend io.ReadWriteCloser
}

type machineOption func(*Machine)

func NewMachine(opts ...machineOption) *Machine {
	machine := &Machine{
		seed:    primitive.NewValue(),
		prompt:  primitive.NewValue(),
		backend: compute.NewBackend(),
	}

	for _, opt := range opts {
		opt(machine)
	}

	// 1. The Core Reactor Pipeline
	// Data flows: Seed -> Backend
	reactor := workflow.NewPipeline(machine.seed, machine.backend)

	// 2. The Ingestion Loop
	// Continuously stream the raw dataset into the seed.
	go func() {
		// We write directly to the seed. The seed absorbs bytes into its operand.
		io.Copy(machine.seed, machine.dataset)
	}()

	// 3. The Feedback & Observation Loop
	// We read from the reactor (which pulls from Backend, which pulls from Seed).
	// The output of the reactor is the newly folded Value.
	// We use Feedback to write this new Value back into the Seed (for further folding)
	// AND we write it to the Prompt (so the outside world can read it).

	// Create a feedback loop that reads from the reactor.
	// The TeeReader will read from reactor, and write a copy to the seed.
	feedback := workflow.NewFeedback(reactor, machine.seed)

	go func() {
		for {
			// We copy from the feedback loop into the prompt.
			// This triggers feedback.Read(), which:
			// 1. Reads a folded Value from the reactor (Backend).
			// 2. Writes a copy of that Value back into the Seed.
			// 3. Returns the Value to io.Copy, which writes it to the Prompt.
			io.Copy(machine.prompt, feedback)
		}
	}()

	return machine
}

func (machine *Machine) Read(p []byte) (n int, err error) {
	return machine.dataset.Read(p)
}

func (machine *Machine) Write(p []byte) (n int, err error) {
	return machine.prompt.Write(p)
}

func (machine *Machine) Close() error {
	return machine.dataset.Close()
}

func WithDataset(dataset io.ReadCloser) machineOption {
	return func(m *Machine) {
		m.dataset = dataset
	}
}
