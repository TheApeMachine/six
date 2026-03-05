package task

import (
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
	Prompts() *vm.Loader
	Holdout() (int, vm.HoldoutType)
	AddResult(vm.SpanResult)
	Outcome() (any, gc.Assertion, any)
	TableData() []map[string]any
}

type Pipeline struct {
	machine    *vm.Machine
	experiment PipelineExperiment
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

	pipeline.machine = vm.NewMachine(
		vm.MachineWithLoader(
			vm.NewLoader(
				vm.LoaderWithStore(
					store.NewLSMSpatialIndex(1.0),
				),
				vm.LoaderWithTokenizer(
					tokenizer.NewUniversal(
						tokenizer.TokenizerWithDataset(pipeline.experiment.Dataset()),
					),
				),
			),
		),
	)

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	pipeline.machine.Start()

	var prompt []data.Chord
	prompts := pipeline.experiment.Prompts()
	prompts.Holdout(pipeline.experiment.Holdout())

	for chord := range prompts.Generate() {
		if chord.ActiveCount() == 0 {
			pipeline.Prompt(prompt)
			prompt = prompt[:0]
		} else {
			prompt = append(prompt, chord)
		}
	}

	if len(prompt) > 0 {
		pipeline.Prompt(prompt)
	}

	return nil
}

func (pipeline *Pipeline) Prompt(prompt []data.Chord) {
	for res := range pipeline.machine.Prompt(prompt, nil) {
		pipeline.experiment.AddResult(res)
	}
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
