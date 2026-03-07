package task

import (
	"fmt"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Pipeline struct {
	machine    *vm.Machine
	experiment tools.PipelineExperiment
	prompts    *tokenizer.Prompt
	testIdx    int
	chordMap   map[data.Chord]byte
	scoreWgts  tools.ScoreWeights
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

	for {
		prompt := pipeline.prompts.Next()

		if prompt == nil {
			break
		}

		pipeline.prompt(prompt)
	}

	if err := pipeline.experiment.Finalize(pipeline.machine.Substrate()); err != nil {
		return fmt.Errorf("experiment finalize: %w", err)
	}

	// Generate artifacts
	for _, artifact := range pipeline.experiment.Artifacts() {
		switch artifact.Type {
		case tools.ArtifactTable:
			if err := WriteTable(artifact.Data, artifact.FileName, pipeline.experiment.Section()); err != nil {
				return fmt.Errorf("write table artifact %s: %w", artifact.FileName, err)
			}
		case tools.ArtifactBarChart:
			data, ok := artifact.Data.([]tools.ExperimentalData)
			if !ok {
				// Fallback or skip
				continue
			}
			series := []projector.BarSeries{
				{Name: "Exact", Data: extractScores(data, "Exact")},
				{Name: "Partial", Data: extractScores(data, "Partial")},
				{Name: "Fuzzy", Data: extractScores(data, "Fuzzy")},
				{Name: "Weighted", Data: extractScores(data, "Weighted")},
			}
			xAxis := make([]string, len(data))
			for i, d := range data {
				xAxis[i] = d.Name
			}
			if err := WriteBarChart(xAxis, series, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, pipeline.experiment.Section()); err != nil {
				return fmt.Errorf("write bar chart artifact %s: %w", artifact.FileName, err)
			}
		case tools.ArtifactConfusionMatrix:
			// Implementation for confusion matrix...
		case tools.ArtifactComboChart:
			data, ok := artifact.Data.(map[string]any)
			if !ok {
				continue
			}
			xAxis := data["xAxis"].([]string)
			series := data["series"].([]projector.ComboSeries)
			xName := data["xName"].(string)
			yName := data["yName"].(string)
			yMin := data["yMin"].(float64)
			yMax := data["yMax"].(float64)

			if err := WriteComboChart(xAxis, series, xName, yName, yMin, yMax, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, pipeline.experiment.Section()); err != nil {
				return fmt.Errorf("write combo chart artifact %s: %w", artifact.FileName, err)
			}
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
	if len(bRes) >= len(bPrompt) {
		generated = bRes[len(bPrompt):]
	} else {
		generated = []byte{}
	}

	console.Info("OBSERVED")
	fmt.Println()
	fmt.Printf("%q\n", string(generated))
	fmt.Println()

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:      pipeline.testIdx,
		Name:     pipeline.experiment.Name(),
		Prefix:   bPrompt,
		Holdout:  []byte(heldOut),
		Observed: generated,
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

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
