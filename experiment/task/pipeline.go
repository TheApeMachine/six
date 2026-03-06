package task

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type PipelineExperiment interface {
	Name() string
	Dataset() provider.Dataset
	Prompts() *tokenizer.Prompt
	Holdout() (int, tokenizer.HoldoutType)
	AddResult(map[string]any)
	Outcome() (any, gc.Assertion, any)
	TableData() []map[string]any
}

type Pipeline struct {
	machine    *vm.Machine
	experiment PipelineExperiment
	loader     *vm.Loader
}

type pipelineOpts func(*Pipeline)

func NewPipeline(opts ...pipelineOpts) (*Pipeline, error) {
	pipeline := &Pipeline{}

	for _, opt := range opts {
		opt(pipeline)
	}

	if pipeline.experiment == nil {
		return nil, PipelineError("missing experiment: use PipelineWithExperiment")
	}

	pf := store.NewPrimeField()

	loader := vm.NewLoader(
		vm.LoaderWithStore(
			store.NewLSMSpatialIndex(1.0),
		),
		vm.LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(pipeline.experiment.Dataset()),
			),
		),
		vm.LoaderWithPrimeField(pf),
	)

	pipeline.machine = vm.NewMachine(
		vm.MachineWithLoader(loader),
		vm.MachineWithPrimeField(pf),
	)

	pipeline.loader = loader

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	pipeline.machine.Start()

	prompts := pipeline.experiment.Prompts()

	for {
		prompt := prompts.Next()

		if prompt == nil {
			break
		}

		pipeline.Prompt(prompt)
	}

	return nil
}

func (pipeline *Pipeline) Prompt(prompt []data.Chord) {
	testIdx := 0

	var bRes []byte
	for res := range pipeline.machine.Prompt(prompt, nil) {
		bRes = append(bRes, res)
	}

	fmt.Println(string(bRes))

	var bPrompt []byte
	for _, chord := range prompt {
		for b := range 256 {
			if chord == data.BaseChord(byte(b)) {
				bPrompt = append(bPrompt, byte(b))
				break
			}
		}
	}

	limit := min(len(bRes), len(bPrompt))

	pipeline.experiment.AddResult(map[string]any{
		"testIdx":    testIdx,
		"experiment": pipeline.experiment.Name(),
		"prompt":     bPrompt,
		"result":     bRes[:limit],
	})
}

func PipelineWithExperiment(experiment PipelineExperiment) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.experiment = experiment
	}
}

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
