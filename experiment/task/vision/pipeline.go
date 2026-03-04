package vision

import (
	"fmt"

	"github.com/theapemachine/six/console"
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
	// Split exactly into masked halves
	pipeline.loader.Holdout(50, 5) 

	var results []ReconstructionResult
	
	// Access the visionCorpus globally within the package
	corpus, names := visionCorpus()
	eigenTable := buildEigenMode(corpus)
	
	sampleIdx := 0
	for prompt := range pipeline.loader.Prompts() {
		originalImage := corpus[sampleIdx]
		name := names[sampleIdx]
		console.Info(fmt.Sprintf("--- Reconstructing %s (Prompt length: %d) ---", name, len(prompt)))
		
		var generatedOutput []byte
		
		promptBytes := make([]byte, len(prompt))
		for i, tok := range prompt {
			promptBytes[i] = byte(tok.TokenID >> 24)
		}

		for res := range pipeline.machine.Prompt(prompt) {
			if len(prompt) + len(generatedOutput) >= len(originalImage) {
				break
			}
			
			// Extract symbol from Morton Key natively via the architecture
			symbol := byte(res.Key >> 24)
			generatedOutput = append(generatedOutput, symbol)
		}

		fullOutput := append(promptBytes, generatedOutput...)
		if len(fullOutput) > len(originalImage) {
			fullOutput = fullOutput[:len(originalImage)]
		}

		matches := false
		if len(fullOutput) == len(originalImage) {
			matches = true
			for i := range fullOutput {
				if fullOutput[i] != originalImage[i] {
					matches = false
					break
				}
			}
		}

        anchorPhase, _ := weightedCircularMean(eigenTable, promptBytes)
		closed := IsGeometricallyClosed(eigenTable, fullOutput, anchorPhase)

		results = append(results, ReconstructionResult{
			Name:      name,
			TargetLen: len(originalImage),
			Generated: len(fullOutput),
			Matches:   matches,
			Steps:     len(generatedOutput),
			IsClosed:  closed,
		})
		
		sampleIdx++
		if sampleIdx >= len(names) {
		    break
		}
	}
	
	return results
}
