package test

import (
	"context"

	"github.com/theapemachine/six/pkg/system/vm"
)

/*
TestHelper wraps a Machine for use in integration tests.
Call SetDataset to ingest a corpus before issuing prompts.
*/
type TestHelper struct {
	ctx     context.Context
	cancel  context.CancelFunc
	Machine *vm.Machine
}

/*
NewTestHelper instantiates a projected TestHelper with a live Machine.
This preserves the existing integration-test behavior while the default
Machine runtime stays native-only.
*/
func NewTestHelper() *TestHelper {
	ctx, cancel := context.WithCancel(context.Background())

	return &TestHelper{
		ctx:    ctx,
		cancel: cancel,
		Machine: vm.NewMachine(
			vm.MachineWithContext(ctx),
		),
	}
}

/*
Teardown cancels the test context.
*/
func (helper *TestHelper) Teardown() {
	helper.Machine.Close()
}
