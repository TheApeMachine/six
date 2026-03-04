package codegen

import (
	"fmt"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Pipeline struct {
	machine *vm.Machine
	loader  *vm.Loader
}

func NewPipeline() *Pipeline {
	pipeline := &Pipeline{
		loader: vm.NewLoader(
			vm.LoaderWithStore(
				store.NewLSMSpatialIndex(1.0),
			),
			vm.LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(
						huggingface.New(
							huggingface.DatasetWithRepo("code-rag-bench/mbpp"),
							huggingface.DatasetWithSamples(1000),
							huggingface.DatasetWithTextColumn("code"),
						),
					),
				),
			),
		),
	}

	pipeline.machine = vm.NewMachine(
		vm.MachineWithLoader(pipeline.loader),
	)

	return pipeline
}

func (pipeline *Pipeline) Run() {
	pipeline.machine.Start()
	pipeline.loader.Holdout(50, 5) // Set 50% holdout and split into 5 samples

	sampleIdx := 0
	for prompt := range pipeline.loader.Prompts() {
		console.Info(fmt.Sprintf("--- Sample %d (Prompt length: %d) ---", sampleIdx, len(prompt)))
		sampleIdx++
		count := 0
		
		var generatedOutput []byte

		for res := range pipeline.machine.Prompt(prompt) {
			if count > 100 {
				break
			}
			// Skip printing the prompt baseline tokens.
			if res.Score == 1.0 && res.Scale == 1 {
				continue
			}

			// Extract the symbol from the Morton Key
			// Encode did (symbol << 24) | pos
			symbol := byte(res.Key >> 24)
			generatedOutput = append(generatedOutput, symbol)

			count++
		}
		
		if len(generatedOutput) > 0 {
			console.Info(fmt.Sprintf("Generated Code (first %d tokens):", len(generatedOutput)))
			// Print the raw generated text
			console.Info(string(generatedOutput))
		} else {
			console.Info("No new code generated.")
		}
	}
}
