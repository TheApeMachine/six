package task

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Pipeline struct {
	ctx        context.Context
	cancel     context.CancelFunc
	pool       *pool.Pool
	broadcast  *pool.BroadcastGroup
	loader     *vm.Loader
	coder      *tokenizer.MortonCoder
	booter     *vm.Booter
	experiment tools.PipelineExperiment
	prompts    *tokenizer.Prompt
	testIdx    int
	scoreWgts  tools.ScoreWeights
	reporter   Reporter
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

	pipeline.coder = tokenizer.NewMortonCoder()
	pipeline.loader = vm.NewLoader(
		vm.LoaderWithStore(store.NewLSMSpatialIndex(0)),
		vm.LoaderWithPrimeField(store.NewPrimeField()),
		vm.LoaderWithPool(workerPool),
		vm.LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(pipeline.experiment.Dataset()),
			),
		),
	)

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	if err := pipeline.loader.Start(); err != nil {
		return fmt.Errorf("loader start: %w", err)
	}

	pipeline.booter.Start()
	defer pipeline.booter.Stop()

	pipeline.prompts = pipeline.experiment.Prompts()

	for pipeline.prompts != nil {
		prompt := pipeline.prompts.Next()

		if prompt == nil {
			break
		}

		pipeline.prompt(prompt)
	}

	if err := pipeline.experiment.Finalize(pipeline.loader.Substrate()); err != nil {
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
	_, _ = pipeline.experiment.Holdout()
	heldOut := pipeline.prompts.HeldOut(pipeline.testIdx)

	resCh := pipeline.broadcast.Subscribe("pipeline-prompt", 10)
	defer pipeline.broadcast.Unsubscribe("pipeline-prompt")

	pipeline.broadcast.Send(
		pool.NewResult(*pool.NewPoolValue(
			pool.WithKey[[]data.Chord]("prompt"),
			pool.WithValue(promptChords),
		)),
	)

wait_result:
	for {
		select {
		case res := <-resCh:
			if res != nil && res.Value != nil {
				if pv, ok := res.Value.(pool.PoolValue[[]data.Chord]); ok {
					if pv.Key == "results" {
						chordRes = pv.Value
						break wait_result
					}
				}
			}
		case <-time.After(2 * time.Second):
			// Cortex didn't respond in time or hasn't converged
			break wait_result
		}
	}

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

	lsmCount := 0
	if lsm, ok := pipeline.loader.Store().(*store.LSMSpatialIndex); ok && lsm != nil {
		lsmCount = lsm.Count()
	}
	var dbgActive []int
	for _, chord := range chordRes {
		dbgActive = append(dbgActive, chord.ActiveCount())
	}
	if len(dbgActive) > 5 {
		dbgActive = dbgActive[:5]
	}

	readout := pipeline.loader.Substrate().Retrieve(
		promptFilter(promptChords),
		geometry.NewPhaseDial().EncodeFromChords(promptChords),
		50,
	)

	console.Info("OBSERVED TEXT DEBUG", "lsmCount", lsmCount, "substrate", len(pipeline.loader.Substrate().Entries), "cortex", len(chordRes), "active", dbgActive, "readout", len(readout))

	outBytes := pipeline.decodeReadout(readout)

	console.Info("OBSERVED TEXT", "text", string(outBytes))

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:      pipeline.testIdx,
		Name:     pipeline.experiment.Name(),
		Prefix:   []byte(pipeline.prompts.Value(pipeline.testIdx)),
		Holdout:  []byte(heldOut),
		Observed: outBytes,
	})

	pipeline.testIdx++
}

func promptFilter(chords []data.Chord) data.Chord {
	var filter data.Chord

	for _, chord := range chords {
		filter = data.ChordOR(&filter, &chord)
	}

	return filter
}

func (pipeline *Pipeline) decodeReadout(readout []data.Chord) []byte {
	var out []byte

	for _, chord := range readout {
		out = append(out, chord.Byte())
	}

	return out
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
