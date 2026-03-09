package task

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Pipeline struct {
	machine    *vm.Machine
	experiment tools.PipelineExperiment
	prompts    *tokenizer.Prompt
	testIdx    int
	scoreWgts  tools.ScoreWeights
	reporter   Reporter
}

type pipelineOpts func(*Pipeline)

func NewPipeline(opts ...pipelineOpts) (*Pipeline, error) {
	pipeline := &Pipeline{
		scoreWgts: tools.DefaultScoreWeights(),
	}

	for _, opt := range opts {
		opt(pipeline)
	}

	if pipeline.experiment == nil {
		return nil, PipelineError("missing experiment: use PipelineWithExperiment")
	}

	if pipeline.reporter == nil {
		pipeline.reporter = NewProjectorReporter()
	}

	pipeline.machine = vm.NewMachine(
		vm.MachineWithLoader(
			vm.NewLoader(
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
	if err := pipeline.machine.Start(); err != nil {
		return fmt.Errorf("machine start: %w", err)
	}

	pipeline.prompts = pipeline.experiment.Prompts()

	for pipeline.prompts != nil {
		prompt := pipeline.prompts.Next()

		if prompt == nil {
			break
		}

		pipeline.prompt(prompt)
	}

	if err := pipeline.experiment.Finalize(nil); err != nil {
		return fmt.Errorf("experiment finalize: %w", err)
	}

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return fmt.Errorf("write results snapshot: %w", err)
	}

	for _, artifact := range pipeline.experiment.Artifacts() {
		if err := pipeline.reporter.WriteArtifact(
			pipeline.experiment, artifact,
		); err != nil {
			return fmt.Errorf(
				"write %s artifact %s: %w",
				artifact.Type,
				artifact.FileName,
				err,
			)
		}
	}

	return nil
}

func extractScores(data []tools.ExperimentalData, field string) []float64 {
	scores := make([]float64, len(data))

	for i, d := range data {
		switch field {
		case "Exact":
			scores[i] = d.Scores.Exact
		case "Partial":
			scores[i] = d.Scores.Partial
		case "Fuzzy":
			scores[i] = d.Scores.Fuzzy
		case "Weighted":
			scores[i] = d.WeightedTotal
		}
	}

	return scores
}

func formatLogPayload(payload string) string {
	if payload == "" {
		return `""`
	}

	if !utf8.ValidString(payload) {
		return strconv.QuoteToASCII(payload)
	}

	for _, r := range payload {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}

		if !unicode.IsPrint(r) {
			return strconv.QuoteToASCII(payload)
		}
	}

	return payload
}

func (pipeline *Pipeline) prompt(promptChords []data.Chord) {
	var chordRes []data.Chord

	// Use cortex Think() for reasoning tasks (holdout=0), Prompt() for recall.
	holdout, _ := pipeline.experiment.Holdout()

	if holdout == 0 {
		chordRes = <-pipeline.machine.Think(promptChords)
	} else {
		chordRes = <-pipeline.machine.Prompt(promptChords)
	}

	heldOut := pipeline.prompts.HeldOut(pipeline.testIdx)

	console.Info("PROMPT")
	fmt.Println()
	fmt.Println(formatLogPayload(pipeline.prompts.Value(pipeline.testIdx)))
	fmt.Println()

	if heldOut != "" {
		console.Info("HOLDOUT")
		fmt.Println()
		fmt.Println(formatLogPayload(heldOut))
		fmt.Println()
	}

	console.Info("OBSERVED",
		"chords", len(chordRes),
	)

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:  pipeline.testIdx,
		Name: pipeline.experiment.Name(),
	})

	pipeline.testIdx++
}

func PipelineWithExperiment(experiment tools.PipelineExperiment) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.experiment = experiment
	}
}

func PipelineWithScoreWeights(weights tools.ScoreWeights) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.scoreWgts = weights
	}
}

func PipelineWithReporter(reporter Reporter) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.reporter = reporter
	}
}

func PipelineWithSnapshotReporter() pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.reporter = NewSnapshotReporter()
	}
}

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
