package task

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type PipelineExperiment interface {
	Name() string
	Section() string
	Dataset() provider.Dataset
	Prompts() *tokenizer.Prompt
	Holdout() (int, tokenizer.HoldoutType)
	AddResult(tools.ExperimentalData)
	Outcome() (any, gc.Assertion, any)
	TableData() []tools.ExperimentalData
}

type Pipeline struct {
	machine    *vm.Machine
	experiment PipelineExperiment
	loader     *vm.Loader
	primefield *store.PrimeField
	prompts    *tokenizer.Prompt
	testIdx    int
	chordMap   map[data.Chord]byte
	// TODO: thread scoreWgts into prompt/result scoring once Pipeline owns
	// WeightedTotal computation instead of experiments doing it ad hoc.
	scoreWgts tools.ScoreWeights
}

type pipelineOpts func(*Pipeline)

func NewPipeline(opts ...pipelineOpts) (*Pipeline, error) {
	pipeline := &Pipeline{
		chordMap:  make(map[data.Chord]byte),
		scoreWgts: tools.DefaultScoreWeights(),
	}

	for b := range 256 {
		pipeline.chordMap[data.BaseChord(byte(b))] = byte(b)
	}

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
	pipeline.primefield = pf

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	pipeline.machine.Start()

	// Train EigenMode co-occurrence tables from the data just loaded.
	if err := pipeline.primefield.BuildEigenModes(); err != nil {
		return fmt.Errorf("BuildEigenModes: %w", err)
	}
	if pipeline.loader != nil && pipeline.loader.Tokenizer() != nil {
		pipeline.loader.Tokenizer().Sequencer().SetEigenMode(pipeline.primefield.EigenMode())
	}

	pipeline.prompts = pipeline.experiment.Prompts()

	for {
		prompt := pipeline.prompts.Next()

		if prompt == nil {
			break
		}

		pipeline.prompt(prompt)
	}

	return nil
}

func (pipeline *Pipeline) prompt(promptChords []data.Chord) {
	var bRes []byte

	for res := range pipeline.machine.Prompt(promptChords, nil) {
		bRes = append(bRes, res)
	}

	// Reconstruct the prompt bytes from input chords
	var bPrompt []byte
	for _, chord := range promptChords {
		if b, ok := pipeline.chordMap[chord]; ok {
			bPrompt = append(bPrompt, b)
		}
	}

	heldOut := pipeline.prompts.HeldOut(pipeline.testIdx)

	console.Info("PROMPT")
	fmt.Println()
	fmt.Println(pipeline.prompts.Value(pipeline.testIdx))
	fmt.Println()
	if heldOut != "" {
		console.Info("HOLDOUT")
		fmt.Println()
		fmt.Println(heldOut)
		fmt.Println()
	}

	// Extract the generated portion (everything after the prompt echo)
	var generated []byte
	if len(bRes) > len(bPrompt) {
		generated = bRes[len(bPrompt):]
	}

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:      pipeline.testIdx,
		Name:     pipeline.experiment.Name(),
		Prefix:   bPrompt,
		Holdout:  []byte(heldOut),
		Observed: generated,
	})

	pipeline.testIdx++
}

func PipelineWithExperiment(experiment PipelineExperiment) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.experiment = experiment
	}
}

func PipelineWithScoreWeights(weights tools.ScoreWeights) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.scoreWgts = weights
	}
}

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
