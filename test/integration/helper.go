package integration

import (
	"context"

	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/logic/graph"
	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/provider"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/vm"
)

/*
IntegrationHelper boots a full vm.Machine with the real system stack.
Tests interact exclusively through machine.Prompt and machine.Stop.
*/
type IntegrationHelper struct {
	ctx             context.Context
	cancel          context.CancelFunc
	Machine         *vm.Machine
	promptTokenizer *process.TokenizerServer
}

/*
NewIntegrationHelper constructs the full system and blocks until the
spatial index is populated. The returned Machine is ready for Prompt calls.
*/
func NewIntegrationHelper(
	ctx context.Context,
	dataset provider.Dataset,
) *IntegrationHelper {
	ctx, cancel := context.WithCancel(ctx)

	machine := vm.NewMachine(
		vm.MachineWithContext(ctx),
		vm.MachineWithSystems(
			lsm.NewSpatialIndexServer(
				lsm.WithContext(ctx),
			),
			process.NewTokenizerServer(
				process.TokenizerWithContext(ctx),
				process.TokenizerWithDataset(dataset, false),
			),
			graph.NewMatrixServer(
				graph.MatrixWithContext(ctx),
			),
		),
	)

	machine.Start()

	promptTokenizer := process.NewTokenizerServer(
		process.TokenizerWithContext(ctx),
		process.TokenizerWithDataset(dataset, true),
		process.TokenizerWithCollector(make([][]data.Chord, 1)),
	)

	promptTokenizer.Start(machine.Pool(), nil)

	return &IntegrationHelper{
		ctx:             ctx,
		cancel:          cancel,
		Machine:         machine,
		promptTokenizer: promptTokenizer,
	}
}

/*
NewPrompt creates a process.Prompt wired to the real tokenizer, ready
to be passed to machine.Prompt.
*/
func (helper *IntegrationHelper) NewPrompt(queries []string) *process.Prompt {
	return process.NewPrompt(
		process.PromptWithStrings(queries),
		process.PromptWithTokenizer(helper.promptTokenizer),
	)
}

/*
ContainsExpected iterates over results and returns true if any result
matches the expected string.
*/
func (helper *IntegrationHelper) ContainsExpected(results [][]byte, expected string) bool {
	for _, result := range results {
		if string(result) == expected {
			return true
		}
	}
	return false
}

/*
Teardown cancels the context and stops the machine.
*/
func (helper *IntegrationHelper) Teardown() {
	if helper.cancel != nil {
		helper.cancel()
		helper.cancel = nil
	}
	helper.Machine.Stop()
}
