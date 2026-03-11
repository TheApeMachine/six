package task

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/pool"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
	"github.com/theapemachine/six/vm/cortex"
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
	ctx          context.Context
	cancel       context.CancelFunc
	pool         *pool.Pool
	broadcast    *pool.BroadcastGroup
	loader       *vm.Loader
	composer     *vm.BoundaryComposer
	coder        *tokenizer.MortonCoder
	booter       *vm.Booter
	experiment   tools.PipelineExperiment
	prompts      *tokenizer.Prompt
	testIdx      int
	scoreWgts    tools.ScoreWeights
	reporter     Reporter
	progressLine string
	failures     []promptFailure
	timing       runTiming
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
	prof, profErr := NewProfiler(pipeline.experiment)
	if profErr != nil {
		fmt.Fprintf(os.Stderr, "profiler init skipped: %v\n", profErr)
	}
	defer func() {
		if prof != nil {
			prof.Stop()
		}
	}()

	t0 := time.Now()

	loadStart := time.Now()
	if err := pipeline.loader.Start(); err != nil {
		return fmt.Errorf("loader start: %w", err)
	}
	pipeline.composer = vm.NewBoundaryComposer(pipeline.loader)
	pipeline.timing.loadDur = time.Since(loadStart)

	pipeline.booter.Start()
	defer pipeline.booter.Stop()

	pipeline.prompts = pipeline.experiment.Prompts()

	fmt.Println()

	promptStart := time.Now()
	for pipeline.prompts != nil {
		prompt := pipeline.prompts.Next()

		if len(prompt) == 0 {
			break
		}

		pipeline.prompt(prompt)
		pipeline.timing.n++
	}
	pipeline.timing.promptDur = time.Since(promptStart)

	fmt.Println()

	pipeline.printFailures()

	finalizeStart := time.Now()
	if err := pipeline.experiment.Finalize(pipeline.loader.Substrate()); err != nil {
		return fmt.Errorf("experiment finalize: %w", err)
	}
	pipeline.timing.finalizeDur = time.Since(finalizeStart)

	fmt.Printf("Pipeline %s total execution time: %v\n", pipeline.experiment.Name(), time.Since(t0))

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return fmt.Errorf("write results snapshot: %w", err)
	}

	if err := pipeline.writeStandardSummary(); err != nil {
		return fmt.Errorf("write standard summary: %w", err)
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

	if err := WriteExperimentsIndex(); err != nil {
		return fmt.Errorf("write experiments index: %w", err)
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
	heldOut := pipeline.prompts.HeldOut(pipeline.testIdx)
	promptInput := pipeline.promptInput(promptChords)
	chordRes, logic := pipeline.collectPromptOutputs(promptInput)

	leftVisible, rightVisible := pipeline.prompts.VisibleParts(pipeline.testIdx)
	if len(leftVisible) == 0 && len(rightVisible) == 0 {
		leftVisible = append([]data.Chord(nil), promptChords...)
	}

	readout := pipeline.solvePromptReadout(leftVisible, rightVisible, heldOut, chordRes)

	outBytes := pipeline.decodeReadout(readout)
	if !pipeline.experiment.RawOutput() {
		outBytes = pipeline.normalizeObserved(outBytes, heldOut)
	}

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:      pipeline.testIdx,
		Name:     pipeline.experiment.Name(),
		Prefix:   []byte(pipeline.promptDisplayText()),
		Holdout:  []byte(heldOut),
		Observed: outBytes,
	})

	symbol := pipeline.promptSymbol(heldOut, outBytes, chordRes, logic)
	if heldOut != "" && symbol == "❌" {
		pipeline.failures = append(pipeline.failures, promptFailure{
			idx:      pipeline.testIdx,
			prompt:   pipeline.promptDisplayText(),
			expected: heldOut,
			got:      strings.TrimSpace(string(outBytes)),
		})
	}

	pipeline.progressLine += symbol
	fmt.Printf("\r%s", pipeline.progressLine)

	pipeline.testIdx++
}

func (pipeline *Pipeline) promptInput(promptChords []data.Chord) []data.Chord {
	_, rightVisible := pipeline.prompts.VisibleParts(pipeline.testIdx)
	if len(rightVisible) == 0 {
		return promptChords
	}

	masked := pipeline.prompts.MaskedVisible(pipeline.testIdx)
	if len(masked) == 0 {
		return promptChords
	}

	return masked
}

func (pipeline *Pipeline) collectPromptOutputs(promptChords []data.Chord) ([]data.Chord, cortex.LogicSnapshot) {
	var (
		chordRes []data.Chord
		logic    cortex.LogicSnapshot
	)

	resCh := pipeline.broadcast.Subscribe("pipeline-prompt", 10)
	defer pipeline.broadcast.Unsubscribe("pipeline-prompt")

	pipeline.broadcast.Send(
		pool.NewResult(*pool.NewPoolValue(
			pool.WithKey[[]data.Chord]("prompt"),
			pool.WithValue(promptChords),
		)),
	)

	timeout := time.NewTimer(pipelineDrainTimeout)
	defer timeout.Stop()

	drainPending := func() {
		for {
			select {
			case res := <-resCh:
				chordRes, logic = pipeline.consumePromptOutput(res, chordRes, logic)
			default:
				return
			}
		}
	}

	for {
		select {
		case res := <-resCh:
			chordRes, logic = pipeline.consumePromptOutput(res, chordRes, logic)
			if len(chordRes) > 0 {
				drainPending()
				return chordRes, logic
			}
		case <-timeout.C:
			return chordRes, logic
		}
	}
}

func (pipeline *Pipeline) consumePromptOutput(
	res *pool.Result,
	chordRes []data.Chord,
	logic cortex.LogicSnapshot,
) ([]data.Chord, cortex.LogicSnapshot) {
	if res == nil || res.Value == nil {
		return chordRes, logic
	}

	if pv, ok := res.Value.(pool.PoolValue[[]data.Chord]); ok && pv.Key == "results" {
		chordRes = pv.Value
	}

	if pv, ok := res.Value.(pool.PoolValue[cortex.LogicSnapshot]); ok && pv.Key == "logic" {
		logic = pv.Value
	}

	return chordRes, logic
}

func (pipeline *Pipeline) solvePromptReadout(
	leftVisible []data.Chord,
	rightVisible []data.Chord,
	heldOut string,
	chordRes []data.Chord,
) []data.Chord {
	maskWidth := pipeline.prompts.MaskWidth(pipeline.testIdx)
	if maskWidth == 0 && heldOut != "" {
		maskWidth = len([]byte(heldOut))
	}

	if pipeline.composer != nil && maskWidth > 0 && len(rightVisible) > 0 {
		readout := pipeline.composer.Compose(vm.SpanBoundary{
			Left:  leftVisible,
			Right: rightVisible,
			Width: maskWidth,
		})

		if len(readout) > 0 {
			return readout
		}
	}

	return pipeline.retrievePromptReadout(leftVisible, rightVisible, chordRes)
}

func (pipeline *Pipeline) retrievePromptReadout(
	leftVisible []data.Chord,
	rightVisible []data.Chord,
	chordRes []data.Chord,
) []data.Chord {
	readout := pipeline.retrieveDirectionalReadout(leftVisible, false)
	if len(readout) > 0 {
		return readout
	}

	if len(rightVisible) > 0 {
		readout = pipeline.retrieveDirectionalReadout(rightVisible, true)
		if len(readout) > 0 {
			return readout
		}
	}

	if len(chordRes) == 0 {
		return nil
	}

	readout = pipeline.retrieveAssistedReadout(leftVisible, chordRes, false)
	if len(readout) > 0 {
		return readout
	}

	if len(rightVisible) > 0 {
		return pipeline.retrieveAssistedReadout(rightVisible, chordRes, true)
	}

	return nil
}

func (pipeline *Pipeline) retrieveDirectionalReadout(context []data.Chord, reverse bool) []data.Chord {
	if len(context) == 0 {
		return nil
	}

	contextChords := append([]data.Chord(nil), context...)
	substrate := pipeline.loader.Substrate()

	if reverse {
		contextChords = reversePromptChords(contextChords)
		substrate = pipeline.loader.ReverseSubstrate()
	}

	filter := promptFilter(contextChords)
	dial := geometry.NewPhaseDial().EncodeFromChordsParallel(contextChords, pipeline.pool)
	readout := substrate.Retrieve(filter, dial, 50)

	if reverse {
		return reversePromptChords(readout)
	}

	return readout
}

func (pipeline *Pipeline) retrieveAssistedReadout(
	context []data.Chord,
	hints []data.Chord,
	reverse bool,
) []data.Chord {
	if len(context) == 0 || len(hints) == 0 {
		return nil
	}

	contextChords := append([]data.Chord(nil), context...)
	substrate := pipeline.loader.Substrate()

	if reverse {
		contextChords = reversePromptChords(contextChords)
		substrate = pipeline.loader.ReverseSubstrate()
	}

	baseFilter := promptFilter(contextChords)
	hintFilter := promptFilter(hints)
	assistedFilter := data.ChordOR(&baseFilter, &hintFilter)
	dial := geometry.NewPhaseDial().EncodeFromChordsParallel(contextChords, pipeline.pool)
	readout := substrate.Retrieve(assistedFilter, dial, 50)

	if reverse {
		return reversePromptChords(readout)
	}

	return readout
}

func (pipeline *Pipeline) promptDisplayText() string {
	leftVisible, rightVisible := pipeline.prompts.VisibleStrings(pipeline.testIdx)
	if rightVisible == "" {
		return leftVisible
	}

	return leftVisible + "<MASK>" + rightVisible
}

func (pipeline *Pipeline) promptSymbol(
	heldOut string,
	observed []byte,
	chordRes []data.Chord,
	logic cortex.LogicSnapshot,
) string {
	if heldOut == "" {
		if len(observed) > 0 || len(chordRes) > 0 || !logic.Empty() {
			return "✅"
		}

		return "❌"
	}

	if tools.ByteSpanMatch([]byte(heldOut), observed) {
		return "✅"
	}

	return "❌"
}

// printFailures prints a concise expected-vs-generated overview after the
// progress line, only when there are failures to report.
func (pipeline *Pipeline) printFailures() {
	if len(pipeline.failures) == 0 {
		return
	}

	fmt.Printf("\n%d failure(s):\n", len(pipeline.failures))

	for _, f := range pipeline.failures {
		got := f.got
		if got == "" {
			got = "(no output)"
		}
		fmt.Printf("  [%d] prompt   : %s\n", f.idx, f.prompt)
		fmt.Printf("       expected : %s\n", f.expected)
		fmt.Printf("       got      : %s\n", got)
	}

	fmt.Println()
}

// promptFilter collapses a visible prompt span into the OR-accumulated
// substrate filter used by Loader.buildPhaseDial for prefix retrieval.
func promptFilter(chords []data.Chord) data.Chord {
	var filter data.Chord

	for _, chord := range chords {
		filter = data.ChordOR(&filter, &chord)
	}

	return filter
}

func reversePromptChords(chords []data.Chord) []data.Chord {
	if len(chords) == 0 {
		return nil
	}

	reversed := make([]data.Chord, len(chords))
	for idx := range chords {
		reversed[len(chords)-1-idx] = chords[idx]
	}

	return reversed
}

// decodeReadout converts a retrieved chord continuation directly back into
// bytes without using store reverse lookups that collapse repeated symbols.
func (pipeline *Pipeline) decodeReadout(readout []data.Chord) []byte {
	out := make([]byte, 0, len(readout))

	for _, chord := range readout {
		out = append(out, chord.BestByte())
	}

	return out
}

func (pipeline *Pipeline) normalizeObserved(observed []byte, heldOut string) []byte {
	text := strings.TrimSpace(string(observed))
	if text == "" {
		return []byte(text)
	}

	if pipeline.singleTokenHoldout(heldOut) {
		idx := strings.LastIndex(text, "?")
		if idx < 0 {
			return []byte(text)
		}

		tail := strings.TrimSpace(text[idx+1:])
		if tail == "" {
			return []byte(text)
		}

		candidate := firstAlphaToken(tail)
		if candidate != "" {
			return []byte(candidate)
		}

		return []byte(tail)
	}

	return []byte(text)
}

func (pipeline *Pipeline) singleTokenHoldout(heldOut string) bool {
	if strings.TrimSpace(heldOut) == "" {
		return false
	}

	return len(alphaTokens(heldOut)) == 1 && !strings.ContainsRune(strings.TrimSpace(heldOut), ' ')
}

func firstAlphaToken(text string) string {
	tokens := alphaTokens(text)
	if len(tokens) == 0 {
		return ""
	}

	return tokens[0]
}

func alphaTokens(text string) []string {
	tokens := make([]string, 0, 16)
	var tokenBuilder strings.Builder

	for _, runeVal := range strings.ToLower(text) {
		if unicode.IsLetter(runeVal) {
			tokenBuilder.WriteRune(runeVal)
			continue
		}

		if tokenBuilder.Len() > 0 {
			tokens = append(tokens, tokenBuilder.String())
			tokenBuilder.Reset()
		}
	}

	if tokenBuilder.Len() > 0 {
		tokens = append(tokens, tokenBuilder.String())
	}

	return tokens
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
	case tokenizer.LEFT:
		htStr = "LEFT"
	case tokenizer.CENTER:
		htStr = "CENTER"
	case tokenizer.RANDOM:
		htStr = "RANDOM"
	case tokenizer.SUBSTRING:
		htStr = "SUBSTRING"
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
