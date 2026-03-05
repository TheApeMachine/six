package imagegen

import (
	"fmt"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Pipeline struct {
	machine *vm.Machine
	loader  *vm.Loader
}

// ReconstructionResult tracks image reconstruction experiments.
type ReconstructionResult struct {
	Name      string
	TargetLen int
	Generated int
	Matches   bool
	Steps     int
	IsClosed  bool
}

func NewPipeline(corpus [][]byte) *Pipeline {
	pipeline := &Pipeline{
		loader: vm.NewLoader(
			vm.LoaderWithStore(
				store.NewLSMSpatialIndex(1.0),
			),
			vm.LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(
						local.New(corpus),
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

func (pipeline *Pipeline) Run() []ReconstructionResult {
	pipeline.machine.Start()
	pipeline.loader.Holdout(50, vm.HoldoutLinear)

	corpus, names := visionCorpus()

	var prompt []data.Chord
	for chord := range pipeline.loader.Generate() {
		prompt = append(prompt, chord)
	}

	var results []ReconstructionResult
	for i, name := range names {
		if i >= len(corpus) {
			break
		}

		res := pipeline.processPrompt(prompt, corpus[i], name)
		results = append(results, res)
	}

	return results
}

func (pipeline *Pipeline) processPrompt(prompt []data.Chord, originalImage []byte, name string) ReconstructionResult {
	console.Info(fmt.Sprintf("--- Reconstructing %s (Prompt length: %d) ---", name, len(prompt)))

	var generatedChords []data.Chord

	for res := range pipeline.machine.Prompt(prompt, nil) {
		if len(prompt)+len(generatedChords) >= len(originalImage) {
			break
		}
		generatedChords = append(generatedChords, res.Chord[0])
	}

	return ReconstructionResult{
		Name:      name,
		TargetLen: len(originalImage),
		Generated: len(generatedChords),
		Steps:     len(generatedChords),
	}
}
