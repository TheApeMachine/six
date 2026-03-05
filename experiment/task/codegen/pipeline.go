package codegen

import (
	"fmt"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
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
	pipeline.loader.Holdout(50, vm.HoldoutLinear)

	var prompt []data.Chord
	for chord := range pipeline.loader.Generate() {
		prompt = append(prompt, chord)
	}

	if len(prompt) == 0 {
		console.Info("No prompt chords generated.")
		return
	}

	console.Info(fmt.Sprintf("--- Prompt length: %d chords ---", len(prompt)))

	count := 0
	for res := range pipeline.machine.Prompt(prompt, nil) {
		if count > 100 {
			break
		}

		console.Info(fmt.Sprintf("Chord %d: score=%.3f", res.Index, res.Score))
		count++
	}
}
