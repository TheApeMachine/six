package test

import (
	"context"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm"
)

/*
TestHelper wraps a Machine for use in integration tests.
Call SetDataset to ingest a corpus before issuing prompts.
*/
type TestHelper struct {
	ctx     context.Context
	cancel  context.CancelFunc
	machine *vm.Machine
}

/*
NewTestHelper instantiates a TestHelper with a live Machine.
*/
func NewTestHelper() *TestHelper {
	ctx, cancel := context.WithCancel(context.Background())

	return &TestHelper{
		ctx:    ctx,
		cancel: cancel,
		machine: vm.NewMachine(
			vm.MachineWithContext(ctx),
		),
	}
}

/*
SetDataset ingests a corpus into the machine before querying.
*/
func (h *TestHelper) SetDataset(dataset provider.Dataset) error {
	return h.machine.SetDataset(dataset)
}

/*
Prompt sends a query through the machine and returns the result bytes.
*/
func (h *TestHelper) Prompt(msg string) ([]byte, error) {
	return h.machine.Prompt(msg)
}

/*
Teardown cancels the test context.
*/
func (h *TestHelper) Teardown() {
	h.cancel()
}
