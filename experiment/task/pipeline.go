package task

import (
	"context"
	"runtime"
	"strings"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process"
	"github.com/theapemachine/six/pkg/system/vm"
)

const pipelineDrainTimeout = 2 * time.Second

type promptFailure struct {
	idx      int
	prompt   string
	expected string
	got      string
}

type runTiming struct {
	loadDur     time.Duration
	promptDur   time.Duration
	finalizeDur time.Duration
	n           int // number of prompts processed
}

type Pipeline struct {
	ctx             context.Context
	cancel          context.CancelFunc
	pool            *pool.Pool
	broadcast       *pool.BroadcastGroup
	coder           *data.MortonCoder
	booter          *vm.Booter
	machine         *vm.Machine
	promptTokenizer *process.TokenizerServer
	experiment      tools.PipelineExperiment
	prompts         *process.Prompt
	testIdx         int
	scoreWgts       tools.ScoreWeights
	reporter        Reporter
	progressLine    string
	failures        []promptFailure
	timing          runTiming
}

type pipelineOpts func(*Pipeline)

func NewPipeline(opts ...pipelineOpts) (*Pipeline, error) {
	ctx, cancel := context.WithCancel(context.Background())
	workerPool := pool.New(
		ctx, 1, runtime.NumCPU(), nil,
	)

	pipeline := &Pipeline{
		ctx:       ctx,
		cancel:    cancel,
		pool:      workerPool,
		broadcast: workerPool.CreateBroadcastGroup("broadcast", time.Second*10),
		scoreWgts: tools.DefaultScoreWeights(),
		booter: vm.NewBooter(
			vm.BooterWithContext(ctx),
			vm.BooterWithPool(workerPool),
		),
	}

	for _, opt := range opts {
		opt(pipeline)
	}

	if pipeline.experiment == nil {
		return nil, PipelineError(
			"missing experiment: use PipelineWithExperiment",
		)
	}

	if pipeline.reporter == nil {
		pipeline.reporter = NewProjectorReporter()
	}

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	dataset := pipeline.experiment.Dataset()

	pipeline.machine = vm.NewMachine(
		vm.MachineWithContext(pipeline.ctx),
		vm.MachineWithSystems(
			lsm.NewSpatialIndexServer(
				lsm.WithContext(pipeline.ctx),
			),
			process.NewTokenizerServer(
				process.TokenizerWithContext(pipeline.ctx),
				process.TokenizerWithDataset(dataset, false),
			),
			substrate.NewGraphServer(
				substrate.GraphWithContext(pipeline.ctx),
			),
		),
	)

	pipeline.machine.Start()
	defer pipeline.machine.Stop()

	pipeline.promptTokenizer = process.NewTokenizerServer(
		process.TokenizerWithContext(pipeline.ctx),
		process.TokenizerWithDataset(dataset, true),
		process.TokenizerWithCollector(make([][]data.Chord, 1)),
	)
	pipeline.promptTokenizer.Start(pipeline.machine.Pool(), nil)

	pipeline.prompts = pipeline.experiment.Prompts()
	if pipeline.prompts != nil {
		process.PromptWithTokenizer(pipeline.promptTokenizer)(pipeline.prompts)
		holdoutPrct, holdoutType := pipeline.experiment.Holdout()
		process.PromptWithHoldout(holdoutPrct, holdoutType)(pipeline.prompts)
	}

	testIdx := 0
	for pipeline.prompts.Next() {
		if pipeline.prompts.Error() != nil {
			return pipeline.prompts.Error()
		}

		results, err := pipeline.machine.Prompt(pipeline.prompts)
		if err != nil {
			return err
		}

		var observed []byte
		if len(results) > 0 {
			observed = results[0]
		}

		orig := pipeline.prompts.Original()
		masked := pipeline.prompts.Masked()

		holdoutStr := orig
		if len(orig) > len(masked) {
			holdoutStr = strings.Replace(orig, masked, "", 1)
		}

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      testIdx,
			Name:     pipeline.experiment.Name(),
			Prefix:   []byte(masked),
			Holdout:  []byte(holdoutStr),
			Observed: observed,
		})

		testIdx++
	}

	return pipeline.writeStandardSummary()
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

func (pipeline *Pipeline) writeStandardSummary() error {
	rows, ok := pipeline.experiment.TableData().([]tools.ExperimentalData)
	if !ok || len(rows) == 0 {
		return nil
	}

	holdoutN, holdoutType := pipeline.experiment.Holdout()

	htStr := "RIGHT"
	switch holdoutType {
	case process.LEFT:
		htStr = "LEFT"
	case process.CENTER:
		htStr = "CENTER"
	case process.RANDOM:
		htStr = "RANDOM"
	case process.MATCH:
		htStr = "MATCH"
	}

	return WriteStandardSummary(
		pipeline.experiment.Name(),
		pipeline.experiment.Section(),
		rows,
		holdoutN,
		htStr,
		pipeline.timing,
	)
}

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
